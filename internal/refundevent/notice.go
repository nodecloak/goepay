package refundevent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/platform/jobs"
	"goepay/internal/siteinfo"
)

type NoticeMessage struct {
	Title, Text, HTML string
	seal              [32]byte
}

func (m NoticeMessage) fingerprint() [32]byte {
	raw, _ := encode([]string{m.Title, m.Text, m.HTML})
	return sha256.Sum256(raw)
}
func (m NoticeMessage) Valid() bool    { return m.Title != "" && m.seal == m.fingerprint() }
func (NoticeMessage) String() string   { return "refundevent201.NoticeMessage(rendered)" }
func (NoticeMessage) GoString() string { return "refundevent201.NoticeMessage(rendered)" }

type NoticeSender interface {
	NoticeScope() string
	ValidateNoticeRecipient(string) error
	SendRefundEventSubscriptionClosed(context.Context, string, NoticeMessage) error
}

func approvedDiagnostic(d string) bool {
	switch d {
	case "receiver outcome unavailable", "receiver response incomplete", "receiver response exceeds limit", "receiver acknowledgement missing", "previous delivery result unavailable":
		return true
	}
	if len(d) == len("receiver HTTP status 500") && strings.HasPrefix(d, "receiver HTTP status ") {
		for _, r := range d[len("receiver HTTP status "):] {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// RenderClosedNotice accepts fixed transport diagnostics, never response bodies
// or raw URL/network errors that might contain signing material or credentials.
func RenderClosedNotice(site, endpoint, recentError string, at time.Time) (NoticeMessage, error) {
	var m NoticeMessage
	u, err := url.Parse(endpoint)
	if !text(site, 256) || strings.TrimSpace(site) == "" || !text(endpoint, 500) || err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || !approvedDiagnostic(recentError) || at.IsZero() {
		return m, ErrInvalid
	}
	stamp := at.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
	intro := "尊敬的商户：您的事件消息订阅地址已连续请求失败10次，系统已自动取消全部事件订阅。请检查订阅地址后在商户后台重新开启。"
	m.Title = site + " - 事件消息订阅已关闭"
	m.Text = intro + "\n订阅地址：" + endpoint + "\n最近错误：" + recentError + "\n----------\n" + site + "\n" + stamp
	m.HTML = intro + "<br/><b>订阅地址：</b>" + html.EscapeString(endpoint) + "<br/><b>最近错误：</b>" + html.EscapeString(recentError) + "<br/>----------<br/>" + html.EscapeString(site) + "<br/>" + stamp
	m.seal = m.fingerprint()
	return m, nil
}

type closedNotice struct {
	EventID, MerchantID, ClaimID, Generation                          int64
	Endpoint, Diagnostic, Email, Site, ResultProof, SubscriptionProof string
	At                                                                time.Time
}

func validEmail(email string) bool {
	a, err := mail.ParseAddress(email)
	return err == nil && a.Address == email && text(email, 254)
}

func (s *Store) captureClosedNotice(ctx context.Context, tx pgx.Tx, c deliveryClaim, resultMAC, subMAC []byte, diagnostic string) error {
	if !approvedDiagnostic(diagnostic) {
		return ErrSource
	}
	n := closedNotice{EventID: c.EventID, MerchantID: c.MerchantID, ClaimID: c.ID, Generation: c.Generation, Endpoint: c.Endpoint, Diagnostic: diagnostic, ResultProof: hex.EncodeToString(resultMAC), SubscriptionProof: hex.EncodeToString(subMAC), At: time.Now().UTC()}
	// A profile email is unverified and must never become a recipient.
	err := tx.QueryRow(ctx, `SELECT email FROM enrollment_emails WHERE merchant_id=$1`, c.MerchantID).Scan(&n.Email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if n.Email != "" && !validEmail(n.Email) {
		return ErrSource
	}
	site, err := siteinfo.Load(ctx, tx)
	if err != nil {
		return err
	}
	n.Site = site.Name
	if _, err = RenderClosedNotice(n.Site, n.Endpoint, n.Diagnostic, n.At); err != nil {
		return err
	}
	raw, mac, err := s.seal("closed-notice", n)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_refund_event_closed_notices201(event_id,merchant_id,body,mac) VALUES($1,$2,$3,$4)`, n.EventID, n.MerchantID, raw, mac); err != nil {
		return err
	}
	if err = audit(ctx, tx, n.MerchantID, "closed-notice", mac); err != nil {
		return err
	}
	if n.Email == "" {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES($1,$2,$3::jsonb)`, noticeKey(n.EventID), NoticeJobKind, jobPayload(n.EventID))
	return err
}

type noticeClaim struct {
	EventID, MerchantID, JobID          int64
	JobAttempt                          int
	NoticeProof, ScopeProof, LeaseProof string
	At                                  time.Time
}
type noticeResult struct {
	EventID           int64
	ClaimProof, State string
	At                time.Time
}

func (s *Store) DeliverClosedNotice(ctx context.Context, j jobs.Job, sender NoticeSender, verify VerifyEvent) error {
	id, err := payloadEventID(j.Payload)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	event, _, err := s.readEvent(ctx, tx, id)
	if err != nil {
		return err
	}
	if err = lockMerchant(ctx, tx, event.MerchantID); err != nil {
		return err
	}
	if err = checkJob(ctx, tx, j, NoticeJobKind, id); err != nil {
		return err
	}
	if verify == nil {
		return ErrSource
	}
	if err = verify(ctx, tx, event); err != nil {
		return err
	}
	var n closedNotice
	var raw, mac []byte
	if err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_refund_event_closed_notices201 WHERE event_id=$1 AND merchant_id=$2`, id, event.MerchantID).Scan(&raw, &mac); err != nil {
		return err
	}
	if s.open("closed-notice", raw, mac, &n) != nil || n.EventID != id || n.MerchantID != event.MerchantID {
		return ErrSource
	}
	var resultRaw, resultMAC []byte
	if err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_refund_event_results201 WHERE claim_id=$1`, n.ClaimID).Scan(&resultRaw, &resultMAC); err != nil {
		return err
	}
	var r deliveryResult
	if s.open("result", resultRaw, resultMAC, &r) != nil || n.ResultProof != hex.EncodeToString(resultMAC) || r.EventID != id || (r.State != "failed" && r.State != "unknown") {
		return ErrSource
	}
	var witnessed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_event_subscription_revisions201 WHERE merchant_id=$1 AND mac=$2)`, n.MerchantID, mustDecode(n.SubscriptionProof)).Scan(&witnessed); err != nil {
		return err
	}
	if !witnessed {
		return ErrSource
	}
	var existingRaw, existingMAC []byte
	err = tx.QueryRow(ctx, `SELECT body,mac FROM merchant_refund_event_notice_claims201 WHERE event_id=$1`, id).Scan(&existingRaw, &existingMAC)
	if err == nil {
		var prior noticeClaim
		if s.open("notice-claim", existingRaw, existingMAC, &prior) != nil || prior.EventID != id || prior.NoticeProof != hex.EncodeToString(mac) {
			return ErrSource
		}
		if err = deliveryWitness(ctx, tx, n.MerchantID, id, 0, "notice-claim", existingMAC); err != nil {
			return err
		}
		// An interrupted SMTP send is unknown and is never automatically resent.
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	seen, err := hasDeliveryWitness(ctx, tx, n.MerchantID, id, 0, "notice-claim")
	if err != nil {
		return err
	}
	if seen {
		return ErrSource
	}
	var currentEmail string
	err = tx.QueryRow(ctx, `SELECT email FROM enrollment_emails WHERE merchant_id=$1`, n.MerchantID).Scan(&currentEmail)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if n.Email == "" || currentEmail != n.Email {
		return tx.Commit(ctx)
	}
	if sender == nil || sender.NoticeScope() == "" || sender.ValidateNoticeRecipient(n.Email) != nil {
		return ErrSource
	}
	message, err := RenderClosedNotice(n.Site, n.Endpoint, n.Diagnostic, n.At)
	if err != nil {
		return err
	}
	c := noticeClaim{EventID: id, MerchantID: n.MerchantID, JobID: j.ID, JobAttempt: j.Attempts, NoticeProof: hex.EncodeToString(mac), ScopeProof: hex.EncodeToString(s.sign("notice-scope", []byte(sender.NoticeScope()))), LeaseProof: hex.EncodeToString(s.sign("lease", []byte(j.Token))), At: time.Now().UTC()}
	claimRaw, claimMAC, err := s.seal("notice-claim", c)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_refund_event_notice_claims201(event_id,job_id,body,mac) VALUES($1,$2,$3,$4)`, id, j.ID, claimRaw, claimMAC); err != nil {
		return err
	}
	if err = deliveryAudit(ctx, tx, n.MerchantID, id, 0, "notice-claim", claimMAC); err != nil {
		return err
	}
	if err = checkJob(ctx, tx, j, NoticeJobKind, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	mailCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state := "accepted"
	if sender.SendRefundEventSubscriptionClosed(mailCtx, n.Email, message) != nil {
		state = "unknown"
	}
	result := noticeResult{EventID: id, ClaimProof: hex.EncodeToString(claimMAC), State: state, At: time.Now().UTC()}
	resultRaw, resultMAC, err = s.seal("notice-result", result)
	if err != nil {
		return nil
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	write, err := s.pool.Begin(writeCtx)
	if err != nil {
		return nil
	}
	defer write.Rollback(writeCtx)
	if _, err = write.Exec(writeCtx, `INSERT INTO merchant_refund_event_notice_results201(event_id,body,mac) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, resultRaw, resultMAC); err != nil {
		return nil
	}
	if err = deliveryAudit(writeCtx, write, n.MerchantID, id, 0, "notice-result", resultMAC); err != nil {
		return nil
	}
	_ = write.Commit(writeCtx)
	return nil
}

func mustDecode(value string) []byte {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return nil
	}
	return raw
}
