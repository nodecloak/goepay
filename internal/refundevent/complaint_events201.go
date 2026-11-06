package refundevent

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Complaint events ride the SAME merchant event subscription as refund events
// (one endpoint, one RSA key, one consecutive-failure counter). They are the
// 交易投诉 half of Pro's 订阅事件. ux-complain's provider sync enqueues one event
// per complaint STATE CHANGE via CaptureComplaint; the worker signs and POSTs it
// with the identical transport, retry and private-address rules as refund
// delivery, and a tenth failure auto-disables the whole subscription.

// ComplaintEvent freezes the complaint facts delivered for one (complaint,state).
type ComplaintEvent struct {
	ID, ComplaintID, MerchantID int64
	State                       string
	Content                     string
}

// ComplaintCapture is the minimal input ux-complain hands to CaptureComplaint
// inside its own upsert transaction: the complaint, its owning merchant, the
// coarse state this event reports, and the signed webhook content.
type ComplaintCapture struct {
	ComplaintID, MerchantID int64
	State                   string
	Content                 []byte
}

func validComplaintState(s string) bool {
	return s == "open" || s == "processing" || s == "done"
}

func complaintJobPayload(id int64) []byte {
	raw, _ := json.Marshal(map[string]string{"event_id": fmt.Sprint(id)})
	return raw
}

// CaptureComplaint records one complaint event and enqueues its delivery job,
// inside the caller's transaction. It is idempotent per (complaint_id, state):
// the UNIQUE(complaint_id, local_state) constraint makes a re-sync of the same
// complaint at the same state insert nothing and enqueue no job (ON CONFLICT DO
// NOTHING). A forward state change is a new (complaint,state) row and one job.
// It never changes the complaint sync's own semantics; it only appends.
func (s *Store) CaptureComplaint(ctx context.Context, tx pgx.Tx, in ComplaintCapture) error {
	if in.ComplaintID <= 0 || in.MerchantID <= 0 || !validComplaintState(in.State) || !json.Valid(in.Content) || len(in.Content) == 0 || len(in.Content) > 12000 {
		return ErrSource
	}
	ready, err := Ready(ctx, tx)
	if err != nil || !ready {
		return err
	}
	var present bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.merchant_complaint_events201') IS NOT NULL`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil
	}
	event := ComplaintEvent{ComplaintID: in.ComplaintID, MerchantID: in.MerchantID, State: in.State, Content: string(in.Content)}
	// Reserve the identity id first so the sealed body binds to it, then insert
	// with ON CONFLICT DO NOTHING on (complaint_id, state): a losing re-sync
	// leaves no row and enqueues no job.
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_complaint_events201','id'))`).Scan(&event.ID); err != nil {
		return err
	}
	raw, mac, err := s.seal("complaint-event", event)
	if err != nil {
		return err
	}
	var inserted int64
	err = tx.QueryRow(ctx, `INSERT INTO merchant_complaint_events201(id,complaint_id,local_state,merchant_id,body,mac)
 OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(complaint_id,local_state) DO NOTHING RETURNING id`,
		event.ID, event.ComplaintID, event.State, event.MerchantID, raw, mac).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already captured for this (complaint,state): idempotent no-op.
		return nil
	}
	if err != nil {
		return err
	}
	if err = complaintAudit(ctx, tx, event.MerchantID, event.ID, "complaint-capture", mac); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES($1,$2,$3::jsonb)`, complaintEventKey(event.ID), ComplaintJobKind, complaintJobPayload(event.ID))
	return err
}

func complaintAudit(ctx context.Context, tx pgx.Tx, mid, eventID int64, operation string, proof []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'refund-event201','merchant-refund-events201',$1,$2,jsonb_build_object('proof',$3::text,'complaint_event_id',$4::text))`, mid, operation, hex.EncodeToString(proof), fmt.Sprint(eventID))
	return err
}

func (s *Store) readComplaintEvent(ctx context.Context, tx pgx.Tx, id int64) (ComplaintEvent, []byte, error) {
	var event ComplaintEvent
	var complaint, mid int64
	var state string
	var raw, mac []byte
	err := tx.QueryRow(ctx, `SELECT complaint_id,local_state,merchant_id,body,mac FROM merchant_complaint_events201 WHERE id=$1`, id).Scan(&complaint, &state, &mid, &raw, &mac)
	if err != nil {
		return event, nil, err
	}
	if s.open("complaint-event", raw, mac, &event) != nil || event.ID != id || event.ComplaintID != complaint || event.MerchantID != mid || event.State != state || !json.Valid([]byte(event.Content)) {
		return event, nil, ErrSource
	}
	var witnessed bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation='complaint-capture' AND actor_role='system' AND actor_id=0 AND actor_name='refund-event201' AND changes=jsonb_build_object('proof',$2::text,'complaint_event_id',$3::text)`, mid, hex.EncodeToString(mac), fmt.Sprint(id)).Scan(&witnessed); err != nil {
		return event, nil, err
	}
	if !witnessed {
		return event, nil, ErrSource
	}
	return event, mac, nil
}

type complaintClaim struct {
	ID, EventID, MerchantID, JobID, SubscriptionRevision, Generation int64
	Sequence, JobAttempt                                             int
	Endpoint, SubscriptionProof, EventProof, LeaseProof              string
	At                                                               time.Time
}
type complaintResult struct {
	ClaimID, EventID, MerchantID  int64
	ClaimProof, State, Diagnostic string
	At                            time.Time
}

func (s *Store) lastComplaintClaim(ctx context.Context, tx pgx.Tx, event ComplaintEvent) (complaintClaim, []byte, *complaintResult, []byte, error) {
	var c complaintClaim
	var raw, mac []byte
	var id int64
	var seq int
	err := tx.QueryRow(ctx, `SELECT id,sequence,body,mac FROM merchant_complaint_event_claims201 WHERE event_id=$1 ORDER BY sequence DESC LIMIT 1`, event.ID).Scan(&id, &seq, &raw, &mac)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil, nil, nil, nil
	}
	if err != nil {
		return c, nil, nil, nil, err
	}
	if s.open("complaint-claim", raw, mac, &c) != nil || c.ID != id || c.EventID != event.ID || c.MerchantID != event.MerchantID || c.Sequence != seq {
		return c, nil, nil, nil, ErrSource
	}
	_, claimMAC, err := s.seal("complaint-claim", c)
	if err != nil {
		return c, nil, nil, nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_complaint_event_results201 WHERE claim_id=$1`, id).Scan(&raw, &mac); errors.Is(err, pgx.ErrNoRows) {
		return c, claimMAC, nil, nil, nil
	} else if err != nil {
		return c, nil, nil, nil, err
	}
	var result complaintResult
	if s.open("complaint-result", raw, mac, &result) != nil || result.ClaimID != id || result.EventID != event.ID || result.MerchantID != event.MerchantID || result.ClaimProof != hex.EncodeToString(claimMAC) {
		return c, nil, nil, nil, ErrSource
	}
	switch result.State {
	case "delivered", "failed", "unknown", "not_started", "skipped":
	default:
		return c, nil, nil, nil, ErrSource
	}
	return c, claimMAC, &result, mac, nil
}

func (s *Store) insertComplaintClaim(ctx context.Context, tx pgx.Tx, event ComplaintEvent, eventMAC []byte, state subscription, subMAC []byte, j jobs.Job, last complaintClaim) (complaintClaim, []byte, error) {
	c := complaintClaim{EventID: event.ID, MerchantID: event.MerchantID, JobID: j.ID, JobAttempt: j.Attempts, Sequence: last.Sequence + 1, Endpoint: state.EventURL, SubscriptionRevision: state.Revision, Generation: state.Generation, SubscriptionProof: hex.EncodeToString(subMAC), EventProof: hex.EncodeToString(eventMAC), LeaseProof: hex.EncodeToString(s.sign("lease", []byte(j.Token))), At: time.Now().UTC()}
	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_complaint_event_claims201','id'))`).Scan(&c.ID); err != nil {
		return c, nil, err
	}
	raw, mac, err := s.seal("complaint-claim", c)
	if err != nil {
		return c, nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_complaint_event_claims201(id,event_id,sequence,job_id,body,mac) OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6)`, c.ID, c.EventID, c.Sequence, c.JobID, raw, mac); err != nil {
		return c, nil, err
	}
	if err = complaintAudit(ctx, tx, c.MerchantID, c.EventID, "complaint-claim", mac); err != nil {
		return c, nil, err
	}
	return c, mac, nil
}

func (s *Store) complaintResultTx(ctx context.Context, tx pgx.Tx, c complaintClaim, claimMAC []byte, state, diagnostic string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_complaint_event_results201 WHERE claim_id=$1)`, c.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	r := complaintResult{ClaimID: c.ID, EventID: c.EventID, MerchantID: c.MerchantID, ClaimProof: hex.EncodeToString(claimMAC), State: state, Diagnostic: diagnostic, At: time.Now().UTC()}
	raw, resultMAC, err := s.seal("complaint-result", r)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_complaint_event_results201(claim_id,body,mac) VALUES($1,$2,$3)`, c.ID, raw, resultMAC); err != nil {
		return err
	}
	if err = complaintAudit(ctx, tx, c.MerchantID, c.EventID, "complaint-result", resultMAC); err != nil {
		return err
	}
	sub, previous, err := s.readSubscription(ctx, tx, c.MerchantID)
	if err != nil {
		return err
	}
	if sub.Generation != c.Generation {
		return nil
	}
	sub, _ = applySubscriptionOutcome(sub, c.EventID, state)
	_, err = s.writeSubscription(ctx, tx, sub, previous)
	return err
}

func (s *Store) recordComplaintResult(c complaintClaim, claimMAC []byte, state, diagnostic string) error {
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
	if err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_complaint_event_claims201 WHERE id=$1`, c.ID).Scan(&raw, &mac); err != nil {
		return err
	}
	var saved complaintClaim
	if s.open("complaint-claim", raw, mac, &saved) != nil || saved != c || hex.EncodeToString(mac) != hex.EncodeToString(claimMAC) {
		return ErrSource
	}
	if err = s.complaintResultTx(ctx, tx, c, claimMAC, state, diagnostic); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyComplaintEvent lets the caller re-prove the event's facts against the
// live complaint row before the webhook is signed and sent.
type VerifyComplaintEvent func(context.Context, pgx.Tx, ComplaintEvent) error

// DeliverComplaint signs and POSTs one complaint event with the same transport,
// retry (only pre-transmission failures replay), consecutive-failure counter and
// private-address rejection as refund delivery. Idempotent: a permanent claim
// without a result is an unknown outcome, never a licence to re-send.
func (s *Store) DeliverComplaint(ctx context.Context, j jobs.Job, key *rsa.PrivateKey, client *http.Client, verify VerifyComplaintEvent) error {
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
	event, eventMAC, err := s.readComplaintEvent(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = lockMerchant(ctx, tx, event.MerchantID); err != nil {
		return err
	}
	if err = checkJob(ctx, tx, j, ComplaintJobKind, id); err != nil {
		return err
	}
	if verify == nil {
		return ErrSource
	}
	if err = verify(ctx, tx, event); err != nil {
		return err
	}
	last, lastMAC, result, _, err := s.lastComplaintClaim(ctx, tx, event)
	if err != nil {
		return err
	}
	if last.ID != 0 {
		if result == nil {
			if last.JobID == j.ID && last.JobAttempt == j.Attempts && last.LeaseProof == hex.EncodeToString(s.sign("lease", []byte(j.Token))) {
				return tx.Commit(ctx)
			}
			if err = s.complaintResultTx(ctx, tx, last, lastMAC, "unknown", "previous delivery result unavailable"); err != nil {
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
	active := sub.EventURL != "" && hasEventType(sub.EventTypes, "complain")
	var post string
	if active {
		if httpclient.ValidateURL(sub.EventURL) != nil {
			return ErrInvalid
		}
		params := compat.Params{"event": "complain", "content": event.Content, "pid": fmt.Sprint(event.MerchantID), "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA"}
		params["sign"], err = compat.SignRSA(params, key)
		if err != nil {
			return errors.New("complaint event signing unavailable")
		}
		form := url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
		post = form.Encode()
	}
	c, claimMAC, err := s.insertComplaintClaim(ctx, tx, event, eventMAC, sub, subMAC, j, last)
	if err != nil {
		return err
	}
	if !active {
		if err = s.complaintResultTx(ctx, tx, c, claimMAC, "skipped", "complaint subscription disabled"); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	sub.LastState = "delivering"
	sub.EventID = event.ID
	if _, err = s.writeSubscription(ctx, tx, sub, subMAC); err != nil {
		return err
	}
	if err = checkJob(ctx, tx, j, ComplaintJobKind, id); err != nil {
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
		if s.recordComplaintResult(c, claimMAC, "not_started", "request construction failed") != nil {
			return ErrSource
		}
		return errors.New("complaint event request construction failed")
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
		if err = s.recordComplaintResult(c, claimMAC, "not_started", "connection not established"); err != nil {
			return err
		}
		return errors.New("complaint event connection not established")
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
	if s.recordComplaintResult(c, claimMAC, state, diagnostic) != nil {
		return errors.New("complaint event delivery result unavailable")
	}
	return nil
}
