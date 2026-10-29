package refundevent201

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/compat"
	"goepay/internal/platform/httpclient"
	"goepay/internal/platform/jobs"
)

type deliveryClaim struct {
	ID, EventID, MerchantID, JobID, SubscriptionRevision, Generation    int64
	Sequence, JobAttempt                                                int
	Endpoint, SubscriptionProof, EventProof, LeaseProof, PreviousResult string
	At                                                                  time.Time
}
type deliveryResult struct {
	ClaimID, EventID, MerchantID  int64
	ClaimProof, State, Diagnostic string
	At                            time.Time
}

func (s *Store) lastClaim(ctx context.Context, tx pgx.Tx, event Event) (deliveryClaim, []byte, *deliveryResult, []byte, error) {
	var c deliveryClaim
	var raw, mac []byte
	var id int64
	var seq, count int
	err := tx.QueryRow(ctx, `SELECT id,sequence,body,mac FROM merchant_refund_event_claims201 WHERE event_id=$1 ORDER BY sequence DESC LIMIT 1`, event.ID).Scan(&id, &seq, &raw, &mac)
	if errors.Is(err, pgx.ErrNoRows) {
		seen, e := hasDeliveryWitness(ctx, tx, event.MerchantID, event.ID, 0, "claim")
		if e != nil {
			return c, nil, nil, nil, e
		}
		if seen {
			return c, nil, nil, nil, ErrSource
		}
		return c, nil, nil, nil, nil
	}
	if err != nil {
		return c, nil, nil, nil, err
	}
	if s.open("claim", raw, mac, &c) != nil || c.ID != id || c.EventID != event.ID || c.MerchantID != event.MerchantID || c.Sequence != seq {
		return c, nil, nil, nil, ErrSource
	}
	if err = deliveryWitness(ctx, tx, c.MerchantID, c.EventID, c.ID, "claim", mac); err != nil {
		return c, nil, nil, nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM merchant_refund_event_claims201 WHERE event_id=$1`, event.ID).Scan(&count); err != nil {
		return c, nil, nil, nil, err
	}
	if count != seq {
		return c, nil, nil, nil, ErrSource
	}
	var witnesses int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation='claim' AND changes->>'event_id'=$2`, event.MerchantID, fmt.Sprint(event.ID)).Scan(&witnesses); err != nil {
		return c, nil, nil, nil, err
	}
	if witnesses != count {
		return c, nil, nil, nil, ErrSource
	}
	if err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_refund_event_results201 WHERE claim_id=$1`, id).Scan(&raw, &mac); errors.Is(err, pgx.ErrNoRows) {
		seen, e := hasDeliveryWitness(ctx, tx, c.MerchantID, c.EventID, c.ID, "result")
		if e != nil {
			return c, nil, nil, nil, e
		}
		if seen {
			return c, nil, nil, nil, ErrSource
		}
		_, claimMAC, e := s.seal("claim", c)
		return c, claimMAC, nil, nil, e
	} else if err != nil {
		return c, nil, nil, nil, err
	}
	var result deliveryResult
	if s.open("result", raw, mac, &result) != nil || result.ClaimID != id || result.EventID != event.ID || result.MerchantID != event.MerchantID {
		return c, nil, nil, nil, ErrSource
	}
	if err = deliveryWitness(ctx, tx, c.MerchantID, c.EventID, c.ID, "result", mac); err != nil {
		return c, nil, nil, nil, err
	}
	_, claimMAC, err := s.seal("claim", c)
	if err != nil {
		return c, nil, nil, nil, err
	}
	if result.ClaimProof != hex.EncodeToString(claimMAC) {
		return c, nil, nil, nil, ErrSource
	}
	switch result.State {
	case "delivered", "failed", "unknown", "not_started", "skipped":
	default:
		return c, nil, nil, nil, ErrSource
	}
	return c, claimMAC, &result, mac, nil
}

func (s *Store) insertClaim(ctx context.Context, tx pgx.Tx, event Event, eventMAC []byte, state subscription, subMAC []byte, j jobs.Job, last deliveryClaim, lastResult []byte) (deliveryClaim, []byte, error) {
	c := deliveryClaim{EventID: event.ID, MerchantID: event.MerchantID, JobID: j.ID, JobAttempt: j.Attempts, Sequence: last.Sequence + 1, Endpoint: state.EventURL, SubscriptionRevision: state.Revision, Generation: state.Generation, SubscriptionProof: hex.EncodeToString(subMAC), EventProof: hex.EncodeToString(eventMAC), LeaseProof: hex.EncodeToString(s.sign("lease", []byte(j.Token))), PreviousResult: hex.EncodeToString(lastResult), At: time.Now().UTC()}
	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_refund_event_claims201','id'))`).Scan(&c.ID); err != nil {
		return c, nil, err
	}
	raw, mac, err := s.seal("claim", c)
	if err != nil {
		return c, nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_refund_event_claims201(id,event_id,sequence,job_id,body,mac) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6)`, c.ID, c.EventID, c.Sequence, c.JobID, raw, mac); err != nil {
		return c, nil, err
	}
	if err = deliveryAudit(ctx, tx, c.MerchantID, c.EventID, c.ID, "claim", mac); err != nil {
		return c, nil, err
	}
	return c, mac, nil
}

func (s *Store) resultTx(ctx context.Context, tx pgx.Tx, c deliveryClaim, claimMAC []byte, state, diagnostic string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_refund_event_results201 WHERE claim_id=$1)`, c.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	r := deliveryResult{ClaimID: c.ID, EventID: c.EventID, MerchantID: c.MerchantID, ClaimProof: hex.EncodeToString(claimMAC), State: state, Diagnostic: diagnostic, At: time.Now().UTC()}
	raw, resultMAC, err := s.seal("result", r)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_refund_event_results201(claim_id,body,mac) VALUES($1,$2,$3)`, c.ID, raw, resultMAC); err != nil {
		return err
	}
	if err = deliveryAudit(ctx, tx, c.MerchantID, c.EventID, c.ID, "result", resultMAC); err != nil {
		return err
	}
	sub, previous, err := s.readSubscription(ctx, tx, c.MerchantID)
	if err != nil {
		return err
	}
	if sub.Generation != c.Generation {
		return nil
	}
	var closed bool
	sub, closed = applySubscriptionOutcome(sub, c.EventID, state)
	subMAC, err := s.writeSubscription(ctx, tx, sub, previous)
	if err != nil {
		return err
	}
	if closed {
		return s.captureClosedNotice(ctx, tx, c, resultMAC, subMAC, diagnostic)
	}
	return nil
}

// applySubscriptionOutcome maps a delivery result onto the shared per-merchant
// subscription: it records the last delivery state and advances/resets the
// consecutive-failure counter exactly the same way for refund and complaint
// events. A tenth consecutive failure/unknown auto-disables every subscribed
// event type and sets the public notice (returned closed=true). Pure; the
// caller persists the returned subscription.
func applySubscriptionOutcome(sub subscription, eventID int64, state string) (subscription, bool) {
	sub.EventID = eventID
	if state == "not_started" {
		sub.LastState = "pending"
	} else {
		sub.LastState = state
	}
	if state == "delivered" {
		sub.Failures = 0
	}
	closed := false
	if (state == "failed" || state == "unknown") && len(sub.EventTypes) > 0 {
		sub.Failures++
		if sub.Failures >= 10 {
			sub.EventTypes = []string{}
			sub.Failures = 0
			sub.Notice = "事件消息订阅地址已连续请求失败10次，系统已自动取消全部事件订阅，请检查订阅地址后重新开启。"
			closed = true
		}
	}
	return sub, closed
}

func (s *Store) recordResult(c deliveryClaim, claimMAC []byte, state, diagnostic string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockMerchant(ctx, tx, c.MerchantID); err != nil {
		return err
	}
	var raw, mac []byte
	if err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_refund_event_claims201 WHERE id=$1`, c.ID).Scan(&raw, &mac); err != nil {
		return err
	}
	var saved deliveryClaim
	if s.open("claim", raw, mac, &saved) != nil || saved != c || hex.EncodeToString(mac) != hex.EncodeToString(claimMAC) {
		return ErrSource
	}
	if err = s.resultTx(ctx, tx, c, claimMAC, state, diagnostic); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func certainlyNotStarted(err error, wrote bool) bool {
	if wrote {
		return false
	}
	if errors.Is(err, httpclient.ErrDestination) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var cert *tls.CertificateVerificationError
	return errors.As(err, &cert)
}

// Deliver only retries failures proven to precede request transmission. A
// permanent claim without a result is an unknown outcome, never permission to
// replay a potentially accepted refund notification.
func (s *Store) Deliver(ctx context.Context, j jobs.Job, key *rsa.PrivateKey, client *http.Client, verify VerifyEvent) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrSource
	}
	id, err := payloadEventID(j.Payload)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	event, eventMAC, err := s.readEvent(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = lockMerchant(ctx, tx, event.MerchantID); err != nil {
		return err
	}
	if err = checkJob(ctx, tx, j, JobKind, id); err != nil {
		return err
	}
	if verify == nil {
		return ErrSource
	}
	if err = verify(ctx, tx, event); err != nil {
		return err
	}
	last, lastMAC, result, resultMAC, err := s.lastClaim(ctx, tx, event)
	if err != nil {
		return err
	}
	if last.ID != 0 {
		if result == nil {
			// Another call holding the same live lease is not a new owner and
			// must not decide the first caller's in-flight network outcome.
			if last.JobID == j.ID && last.JobAttempt == j.Attempts && last.LeaseProof == hex.EncodeToString(s.sign("lease", []byte(j.Token))) {
				return tx.Commit(ctx)
			}
			if err = s.resultTx(ctx, tx, last, lastMAC, "unknown", "previous delivery result unavailable"); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		if result.State != "not_started" {
			return tx.Commit(ctx)
		}
		if j.ID != last.JobID || j.Attempts <= last.JobAttempt || hex.EncodeToString(s.sign("lease", []byte(j.Token))) == last.LeaseProof {
			return ErrSource
		}
	}
	sub, subMAC, err := s.readSubscription(ctx, tx, event.MerchantID)
	if err != nil {
		return err
	}
	active := sub.EventURL != "" && hasEventType(sub.EventTypes, "refund")
	var post string
	if active {
		if httpclient.ValidateURL(sub.EventURL) != nil {
			return ErrInvalid
		}
		params := compat.Params{"event": "refund", "content": event.Content, "pid": fmt.Sprint(event.MerchantID), "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA"}
		params["sign"], err = compat.SignRSA(params, key)
		if err != nil {
			return errors.New("refund event signing unavailable")
		}
		form := url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
		post = form.Encode()
	}
	c, claimMAC, err := s.insertClaim(ctx, tx, event, eventMAC, sub, subMAC, j, last, resultMAC)
	if err != nil {
		return err
	}
	if !active {
		if err = s.resultTx(ctx, tx, c, claimMAC, "skipped", "refund subscription disabled"); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	sub.LastState = "delivering"
	sub.EventID = event.ID
	if _, err = s.writeSubscription(ctx, tx, sub, subMAC); err != nil {
		return err
	}
	// Recheck after waiting on all source and subscription locks.
	if err = checkJob(ctx, tx, j, JobKind, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var wrote atomic.Bool
	trace := &httptrace.ClientTrace{WroteHeaders: func() { wrote.Store(true) }, WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(requestCtx, trace), "POST", c.Endpoint, strings.NewReader(post))
	if err != nil {
		if s.recordResult(c, claimMAC, "not_started", "request construction failed") != nil {
			return ErrSource
		}
		return errors.New("refund event request construction failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if client == nil {
		client = httpclient.New()
	}
	bounded := *client
	if bounded.Timeout <= 0 || bounded.Timeout > 6*time.Second {
		bounded.Timeout = 6 * time.Second
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, sendErr := bounded.Do(req)
	state, diagnostic := "unknown", "receiver outcome unavailable"
	if sendErr != nil && certainlyNotStarted(sendErr, wrote.Load()) {
		if err = s.recordResult(c, claimMAC, "not_started", "connection not established"); err != nil {
			return err
		}
		return errors.New("refund event connection not established")
	}
	if sendErr == nil && resp != nil {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			state, diagnostic = "failed", fmt.Sprintf("receiver HTTP status %d", resp.StatusCode)
		} else if readErr != nil {
			state, diagnostic = "unknown", "receiver response incomplete"
		} else if len(body) > 64<<10 {
			state, diagnostic = "failed", "receiver response exceeds limit"
		} else if strings.Contains(string(body), "success") || strings.Contains(string(body), "SUCCESS") || strings.Contains(string(body), "Success") {
			state, diagnostic = "delivered", "receiver acknowledged"
		} else {
			state, diagnostic = "failed", "receiver acknowledgement missing"
		}
	} else if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	// A new queue lease can persist an unknown outcome from the durable claim.
	// It cannot transmit again; the lastClaim branch above consumes that claim.
	if s.recordResult(c, claimMAC, state, diagnostic) != nil {
		return errors.New("refund event delivery result unavailable")
	}
	return nil
}
