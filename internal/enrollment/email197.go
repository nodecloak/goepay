package enrollment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

var ErrEmailActor = errors.New("original email binding session required")
var ErrEmailPassword = errors.New("current password rejected")

type EmailActor197 struct {
	User                identity.User
	Token, CSRF, Source string
}
type EmailMetadata197 struct {
	Email         string `json:"email"`
	Verified      bool   `json:"verified"`
	EmailEnabled  bool   `json:"email_enabled"`
	Requires      string `json:"requires"`
	Fingerprint   string `json:"fingerprint"`
	EmailInterval int    `json:"email_interval"`
}
type EmailFlow197 struct {
	ID             string     `json:"flow_id"`
	Phase          string     `json:"phase"`
	OriginalEmail  string     `json:"original_email"`
	Email          string     `json:"email"`
	Delivery       string     `json:"delivery"`
	OldChallenge   string     `json:"old_challenge_id,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
	Reauthenticate bool       `json:"reauthenticate"`
	SendID         string     `json:"send_id,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
}
type emailBirth197 struct {
	ID            string `json:"id"`
	MerchantID    int64  `json:"merchant_id"`
	Username      string `json:"username"`
	Version       int64  `json:"version"`
	Session       string `json:"session"`
	Source        string `json:"source"`
	OriginalEmail string `json:"original_email"`
	Fingerprint   string `json:"fingerprint"`
	Expires       int64  `json:"expires"`
}
type emailFacts197 struct {
	Email     string `json:"email,omitempty"`
	EmailHash string `json:"email_hash,omitempty"`
	Challenge string `json:"challenge,omitempty"`
	SendID    string `json:"send_id,omitempty"`
	SentAt    int64  `json:"sent_at,omitempty"`
}
type emailState197 struct {
	birth                                              emailBirth197
	phase, delivery, email, oldChallenge, newChallenge string
	revision                                           int
	mac                                                []byte
	sendID                                             string
	sentAt                                             int64
	sends                                              map[string]bool
}

func (v emailState197) public() EmailFlow197 {
	out := EmailFlow197{ID: v.birth.ID, Phase: v.phase, OriginalEmail: v.birth.OriginalEmail, Email: v.email, Delivery: v.delivery, OldChallenge: v.oldChallenge, ExpiresAt: time.UnixMicro(v.birth.Expires).UTC(), Reauthenticate: v.phase == "completed", SendID: v.sendID}
	if v.sentAt > 0 {
		sent := time.UnixMicro(v.sentAt).UTC()
		out.SentAt = &sent
	}
	return out
}
func (s *Service) emailMAC197(label string, parts ...string) []byte {
	return s.digest(append([]string{"merchant-email197", label}, parts...)...)
}
func validFlow197(id string) bool {
	if len(id) != 43 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func emailSource197(source string) (string, error) {
	ip := net.ParseIP(source)
	if ip == nil {
		return "", ErrInvalid
	}
	return ip.String(), nil
}

func (s *Service) emailActor197(ctx context.Context, tx pgx.Tx, a EmailActor197) (time.Time, error) {
	var expiry time.Time
	if a.User.Role != "merchant" || a.User.ID <= 0 || a.User.AuthVersion <= 0 {
		return expiry, ErrEmailActor
	}
	var enabled bool
	if e := tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE`, a.User.ID).Scan(&enabled); e != nil || !enabled {
		if e == nil || errors.Is(e, pgx.ErrNoRows) {
			return expiry, ErrEmailActor
		}
		return expiry, e
	}
	// Every contact transition takes the user write lock before the session lock;
	// completion never upgrades a shared user lock while another flow holds it.
	var username string
	if e := tx.QueryRow(ctx, `SELECT username FROM console_users WHERE username=$1 AND role='merchant' FOR UPDATE`, a.User.Username).Scan(&username); e != nil {
		return expiry, ErrEmailActor
	}
	if e := identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); e != nil {
		return expiry, ErrEmailActor
	}
	h := sha256.Sum256([]byte(a.Token))
	e := tx.QueryRow(ctx, `SELECT expires_at FROM console_sessions WHERE token_hash=$1 AND role='merchant'`, h[:]).Scan(&expiry)
	return expiry, e
}
func (s *Service) readEmailFlow197(ctx context.Context, tx pgx.Tx, id string, lock bool) (emailState197, error) {
	var v emailState197
	var body, mac []byte
	var merchant int64
	query := `SELECT merchant_id,body,mac FROM email_binding_flows197 WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	e := tx.QueryRow(ctx, query, id).Scan(&merchant, &body, &mac)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrConflict
	}
	if e != nil {
		return v, e
	}
	if json.Unmarshal(body, &v.birth) != nil || v.birth.ID != id || v.birth.MerchantID != merchant || !hmac.Equal(mac, s.emailMAC197("birth", string(body))) {
		return v, ErrUnavailable
	}
	v.mac = mac
	v.phase = "new"
	if v.birth.OriginalEmail != "" {
		v.phase = "old"
	}
	rows, e := tx.Query(ctx, `SELECT revision,kind,body,previous_mac,mac FROM email_binding_events197 WHERE flow_id=$1 ORDER BY revision`, id)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var revision int
		var kind string
		var facts, previous, next []byte
		if e = rows.Scan(&revision, &kind, &facts, &previous, &next); e != nil {
			return v, e
		}
		if revision != v.revision+1 || !hmac.Equal(previous, v.mac) || !hmac.Equal(next, s.emailMAC197("event", id, strconv.Itoa(revision), kind, hex.EncodeToString(previous), string(facts))) {
			return v, ErrUnavailable
		}
		var f emailFacts197
		if json.Unmarshal(facts, &f) != nil || v.apply(kind, f) != nil {
			return v, ErrUnavailable
		}
		v.revision = revision
		v.mac = next
	}
	return v, rows.Err()
}
func (v *emailState197) apply(kind string, f emailFacts197) error {
	if v.phase == "completed" || v.phase == "closed" {
		return ErrConflict
	}
	switch kind {
	case "send-old":
		if v.phase != "old" || (v.oldChallenge != "" && (f.SendID == "" || v.delivery == "unknown")) || f.Email != v.birth.OriginalEmail || !validFlow197(f.Challenge) {
			return ErrConflict
		}
		if e := v.recordSend197(f); e != nil {
			return e
		}
		v.oldChallenge = f.Challenge
		v.delivery = "unknown"
	case "sent-old", "failed-old":
		if v.phase != "old" || v.oldChallenge != f.Challenge || v.delivery != "unknown" {
			return ErrConflict
		}
		v.delivery = "sent"
		if kind == "failed-old" {
			v.delivery = "failed"
		}
	case "verify-old":
		if v.phase != "old" || v.oldChallenge != f.Challenge || v.delivery != "sent" {
			return ErrConflict
		}
		v.phase = "new"
		v.delivery = ""
		v.sendID = ""
		v.sentAt = 0
	case "send-new":
		if v.phase != "new" || (v.newChallenge != "" && (f.SendID == "" || v.delivery == "unknown" || v.email != f.Email)) || f.Email == "" || f.Email == v.birth.OriginalEmail || !validFlow197(f.Challenge) {
			return ErrConflict
		}
		if e := v.recordSend197(f); e != nil {
			return e
		}
		v.email = f.Email
		v.newChallenge = f.Challenge
		v.delivery = "unknown"
	case "sent-new", "failed-new":
		if v.phase != "new" || v.newChallenge != f.Challenge || v.delivery != "unknown" {
			return ErrConflict
		}
		v.delivery = "sent"
		if kind == "failed-new" {
			v.delivery = "failed"
		}
	case "complete":
		if v.phase != "new" || v.newChallenge != f.Challenge || v.delivery != "sent" || v.email != f.Email || len(f.EmailHash) != 64 {
			return ErrConflict
		}
		v.phase = "completed"
	case "close":
		v.phase = "closed"
	default:
		return ErrConflict
	}
	return nil
}
func (v *emailState197) recordSend197(f emailFacts197) error {
	if f.SendID != "" && !validFlow197(f.SendID) {
		return ErrConflict
	}
	if f.SentAt <= 0 || f.SentAt >= v.birth.Expires {
		return ErrConflict
	}
	if v.sends == nil {
		v.sends = map[string]bool{}
	}
	if f.SendID != "" {
		if v.sends[f.SendID] {
			return ErrConflict
		}
		v.sends[f.SendID] = true
	}
	v.sendID = f.SendID
	v.sentAt = f.SentAt
	return nil
}
func (s *Service) appendEmail197(ctx context.Context, tx pgx.Tx, v *emailState197, kind string, f emailFacts197) error {
	body, e := json.Marshal(f)
	if e != nil {
		return e
	}
	if e = v.apply(kind, f); e != nil {
		return e
	}
	revision := v.revision + 1
	mac := s.emailMAC197("event", v.birth.ID, strconv.Itoa(revision), kind, hex.EncodeToString(v.mac), string(body))
	if _, e = tx.Exec(ctx, `INSERT INTO email_binding_events197(flow_id,revision,kind,body,previous_mac,mac) VALUES($1,$2,$3,$4,$5,$6)`, v.birth.ID, revision, kind, body, v.mac, mac); e != nil {
		return e
	}
	v.revision = revision
	v.mac = mac
	return nil
}

// Ordinary profile JSON cannot confer recovery authority. After the first
// change, the current claim must also match an authenticated completion chain.
func (s *Service) currentEmail197(ctx context.Context, tx pgx.Tx, merchant int64) (string, error) {
	var email, username, marker string
	var hash []byte
	e := tx.QueryRow(ctx, `SELECT email,email_hash,username,COALESCE(binding_flow197,'') FROM enrollment_emails WHERE merchant_id=$1`, merchant).Scan(&email, &hash, &username, &marker)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return "", e
	}
	if errors.Is(e, pgx.ErrNoRows) {
		email = ""
	}
	var flow string
	x := tx.QueryRow(ctx, `SELECT e.flow_id FROM email_binding_events197 e JOIN email_binding_flows197 f ON f.id=e.flow_id WHERE f.merchant_id=$1 AND e.kind='complete' ORDER BY e.id DESC LIMIT 1`, merchant).Scan(&flow)
	if x != nil && !errors.Is(x, pgx.ErrNoRows) {
		return "", x
	}
	if (x == nil && marker != flow) || (errors.Is(x, pgx.ErrNoRows) && marker != "") {
		return "", ErrUnavailable
	}
	if x == nil {
		v, e := s.readEmailFlow197(ctx, tx, flow, false)
		if e != nil || v.phase != "completed" || v.email != email || v.birth.Username != username {
			return "", ErrUnavailable
		}
	}
	if email != "" && !hmac.Equal(hash, s.digest("email", email)) {
		return "", ErrUnavailable
	}
	return email, nil
}
func (s *Service) emailMetadata197(ctx context.Context, tx pgx.Tx, a EmailActor197) (EmailMetadata197, error) {
	email, e := s.currentEmail197(ctx, tx, a.User.ID)
	if e != nil {
		return EmailMetadata197{}, e
	}
	required := "password"
	if email != "" {
		required = "email"
	}
	fp := hex.EncodeToString(s.emailMAC197("source", strconv.FormatInt(a.User.ID, 10), a.User.Username, strconv.FormatInt(a.User.AuthVersion, 10), email))
	return EmailMetadata197{Email: email, Verified: email != "", EmailEnabled: s.sender != nil, Requires: required, Fingerprint: fp}, nil
}
func (s *Service) EmailMetadata197(ctx context.Context, a EmailActor197) (EmailMetadata197, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return EmailMetadata197{}, e
	}
	defer tx.Rollback(ctx)
	interval, available, e := s.emailDelivery197(ctx, tx)
	if e != nil {
		return EmailMetadata197{}, e
	}
	if _, e = s.emailActor197(ctx, tx, a); e != nil {
		return EmailMetadata197{}, e
	}
	v, e := s.emailMetadata197(ctx, tx, a)
	v.EmailEnabled = available
	v.EmailInterval = interval
	return v, e
}
func (s *Service) emailDelivery197(ctx context.Context, tx pgx.Tx) (int, bool, error) {
	rates, _, e := s.rates.Effective(ctx, tx)
	if e != nil {
		return 0, false, ErrUnavailable
	}
	if s.sender == nil {
		return rates.EmailInterval, false, nil
	}
	if prepare, ok := s.sender.(SenderPreparer); ok {
		sender, e := prepare.PrepareVerification(ctx, tx)
		if e != nil || sender == nil {
			return rates.EmailInterval, false, nil
		}
	}
	return rates.EmailInterval, true, nil
}
func (s *Service) deadlineEmail197(ctx context.Context, tx pgx.Tx, v emailState197, expiry time.Time) error {
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !now.Before(expiry) {
		return ErrEmailActor
	}
	if now.UnixMicro() >= v.birth.Expires {
		return ErrCode
	}
	return nil
}
func (s *Service) ownEmailFlow197(ctx context.Context, tx pgx.Tx, a EmailActor197, id string) (emailState197, time.Time, error) {
	var v emailState197
	expiry, e := s.emailActor197(ctx, tx, a)
	if e != nil {
		return v, expiry, e
	}
	v, e = s.readEmailFlow197(ctx, tx, id, true)
	if e != nil {
		return v, expiry, e
	}
	source, e := emailSource197(a.Source)
	if e != nil {
		return v, expiry, e
	}
	if v.birth.MerchantID != a.User.ID || v.birth.Username != a.User.Username || v.birth.Version != a.User.AuthVersion || v.birth.Session != hex.EncodeToString(s.emailMAC197("session", a.Token, a.CSRF)) || v.birth.Source != hex.EncodeToString(s.digest("source", source)) {
		return v, expiry, ErrEmailActor
	}
	if e = s.deadlineEmail197(ctx, tx, v, expiry); e != nil {
		return v, expiry, e
	}
	if v.phase != "completed" {
		current, e := s.currentEmail197(ctx, tx, a.User.ID)
		if e != nil {
			return v, expiry, e
		}
		if current != v.birth.OriginalEmail {
			return v, expiry, ErrConflict
		}
	}
	return v, expiry, nil
}
func (s *Service) BeginEmail197(ctx context.Context, a EmailActor197, id, fingerprint, password string) (EmailFlow197, error) {
	if !validFlow197(id) || len(fingerprint) != 64 || len(password) > 1024 {
		return EmailFlow197{}, ErrInvalid
	}
	source, e := emailSource197(a.Source)
	if e != nil {
		return EmailFlow197{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return EmailFlow197{}, e
	}
	defer tx.Rollback(ctx)
	_, available, e := s.emailDelivery197(ctx, tx)
	if e != nil {
		return EmailFlow197{}, e
	}
	if !available {
		return EmailFlow197{}, ErrUnavailable
	}
	if e = limit(ctx, tx, s.digest("contact-begin-source197", source), 900, 30); e != nil {
		return EmailFlow197{}, commitRejection(ctx, tx, e)
	}
	if e = limit(ctx, tx, s.digest("contact-begin-actor197", strconv.FormatInt(a.User.ID, 10)), 900, 10); e != nil {
		return EmailFlow197{}, commitRejection(ctx, tx, e)
	}
	expiry, e := s.emailActor197(ctx, tx, a)
	if e != nil {
		return EmailFlow197{}, e
	}
	existing, e := s.readEmailFlow197(ctx, tx, id, true)
	if e == nil {
		if existing.birth.MerchantID != a.User.ID || existing.birth.Session != hex.EncodeToString(s.emailMAC197("session", a.Token, a.CSRF)) || existing.birth.Fingerprint != fingerprint || existing.birth.Source != hex.EncodeToString(s.digest("source", source)) {
			return EmailFlow197{}, ErrConflict
		}
		if e = s.deadlineEmail197(ctx, tx, existing, expiry); e != nil {
			return EmailFlow197{}, e
		}
		return existing.public(), nil
	}
	if !errors.Is(e, ErrConflict) {
		return EmailFlow197{}, e
	}
	meta, e := s.emailMetadata197(ctx, tx, a)
	if e != nil {
		return EmailFlow197{}, e
	}
	if !hmac.Equal([]byte(fingerprint), []byte(meta.Fingerprint)) {
		return EmailFlow197{}, ErrConflict
	}
	if !meta.EmailEnabled {
		return EmailFlow197{}, ErrUnavailable
	}
	if !meta.Verified {
		var hash string
		if e = tx.QueryRow(ctx, `SELECT password_hash FROM console_users WHERE username=$1 AND role='merchant'`, a.User.Username).Scan(&hash); e != nil {
			return EmailFlow197{}, e
		}
		if !identity.CheckPassword(hash, password) {
			return EmailFlow197{}, commitRejection(ctx, tx, ErrEmailPassword)
		}
	}
	var until time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()+interval '10 minutes'`).Scan(&until); e != nil {
		return EmailFlow197{}, e
	}
	v := emailState197{birth: emailBirth197{id, a.User.ID, a.User.Username, a.User.AuthVersion, hex.EncodeToString(s.emailMAC197("session", a.Token, a.CSRF)), hex.EncodeToString(s.digest("source", source)), meta.Email, meta.Fingerprint, until.UnixMicro()}, phase: "new"}
	if meta.Verified {
		v.phase = "old"
	}
	body, _ := json.Marshal(v.birth)
	v.mac = s.emailMAC197("birth", string(body))
	if _, e = tx.Exec(ctx, `INSERT INTO email_binding_flows197(id,merchant_id,body,mac) VALUES($1,$2,$3,$4)`, id, a.User.ID, body, v.mac); e != nil {
		return EmailFlow197{}, mapConflict(e)
	}
	if e = s.deadlineEmail197(ctx, tx, v, expiry); e != nil {
		return EmailFlow197{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return EmailFlow197{}, e
	}
	return v.public(), nil
}
func (s *Service) GetEmail197(ctx context.Context, a EmailActor197, id string, closeFlow bool) (EmailFlow197, error) {
	if !validFlow197(id) {
		return EmailFlow197{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return EmailFlow197{}, e
	}
	defer tx.Rollback(ctx)
	v, expiry, e := s.ownEmailFlow197(ctx, tx, a, id)
	if e != nil {
		return EmailFlow197{}, e
	}
	if closeFlow && v.phase != "closed" {
		if e = s.appendEmail197(ctx, tx, &v, "close", emailFacts197{}); e != nil {
			return EmailFlow197{}, e
		}
	}
	if e = s.deadlineEmail197(ctx, tx, v, expiry); e != nil {
		return EmailFlow197{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return EmailFlow197{}, e
	}
	return v.public(), nil
}

func (s *Service) preflightEmail197(ctx context.Context, tx pgx.Tx, a EmailActor197, id string) (emailState197, string, error) {
	var v emailState197
	source, e := emailSource197(a.Source)
	if e != nil || !validFlow197(id) {
		return v, "", ErrInvalid
	}
	v, e = s.readEmailFlow197(ctx, tx, id, false)
	if e != nil {
		return v, "", e
	}
	if a.User.Role != "merchant" || v.birth.MerchantID != a.User.ID || v.birth.Username != a.User.Username || v.birth.Version != a.User.AuthVersion || v.birth.Session != hex.EncodeToString(s.emailMAC197("session", a.Token, a.CSRF)) || v.birth.Source != hex.EncodeToString(s.digest("source", source)) {
		return v, "", ErrEmailActor
	}
	return v, source, nil
}

func (s *Service) SendEmail197(ctx context.Context, a EmailActor197, id, target, sendID string, old bool) (EmailFlow197, error) {
	if sendID != "" && !validFlow197(sendID) {
		return EmailFlow197{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return EmailFlow197{}, e
	}
	defer tx.Rollback(ctx)
	peek, source, e := s.preflightEmail197(ctx, tx, a, id)
	if e != nil {
		return EmailFlow197{}, e
	}
	phase, purpose, kind, email, existing := "new", "contact-new", "new", target, peek.newChallenge
	if old {
		phase, purpose, kind, email, existing = "old", "contact-old", "old", peek.birth.OriginalEmail, peek.oldChallenge
	}
	email, _, e = normalize(email, source)
	if e != nil {
		return EmailFlow197{}, e
	}
	if peek.phase != phase {
		return EmailFlow197{}, ErrConflict
	}
	if !old && email == peek.birth.OriginalEmail {
		return EmailFlow197{}, ErrInvalid
	}
	if !old && peek.email != "" && peek.email != email {
		return EmailFlow197{}, ErrConflict
	}
	if existing != "" && (sendID == "" || peek.sends[sendID]) {
		v, _, e := s.ownEmailFlow197(ctx, tx, a, id)
		if e != nil {
			return EmailFlow197{}, e
		}
		if !old && v.email != email {
			return EmailFlow197{}, ErrConflict
		}
		return v.public(), nil
	}
	if existing != "" && peek.delivery == "unknown" {
		return EmailFlow197{}, ErrConflict
	}
	if s.sender == nil {
		return EmailFlow197{}, ErrUnavailable
	}
	rates, managed, e := s.rates.Effective(ctx, tx)
	if e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	delivery := s.sender
	if prepare, ok := s.sender.(SenderPreparer); ok {
		delivery, e = prepare.PrepareVerification(ctx, tx)
		if e != nil || delivery == nil {
			return EmailFlow197{}, ErrUnavailable
		}
	}
	if e = limit(ctx, tx, s.digest("send-source", source), 86400, rates.IPDaily); e != nil {
		return EmailFlow197{}, commitRejection(ctx, tx, e)
	}
	if e = limit(ctx, tx, s.digest("send-email", email), 86400, rates.EmailDaily); e != nil {
		return EmailFlow197{}, commitRejection(ctx, tx, e)
	}
	if managed {
		if e = limit(ctx, tx, s.digest("send-source-gap194", source), rates.IPInterval, 1); e != nil {
			return EmailFlow197{}, commitRejection(ctx, tx, e)
		}
	}
	v, expiry, e := s.ownEmailFlow197(ctx, tx, a, id)
	if e != nil {
		return EmailFlow197{}, e
	}
	if v.phase != phase {
		return EmailFlow197{}, ErrConflict
	}
	existing = v.newChallenge
	if old {
		existing = v.oldChallenge
	}
	if existing != "" && (sendID == "" || v.sends[sendID]) {
		if !old && v.email != email {
			return EmailFlow197{}, ErrConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return EmailFlow197{}, e
		}
		return v.public(), nil
	}
	if existing != "" && v.delivery == "unknown" {
		return EmailFlow197{}, ErrConflict
	}
	var recent bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM enrollment_challenges WHERE email_hash=$1 AND created_at>clock_timestamp()-($2::integer*interval '1 second'))`, s.digest("email", email), rates.EmailInterval).Scan(&recent); e != nil {
		return EmailFlow197{}, e
	}
	if recent {
		return EmailFlow197{}, commitRejection(ctx, tx, ErrLimited)
	}
	challenge, e := token()
	if e != nil {
		return EmailFlow197{}, e
	}
	n, e := rand.Int(rand.Reader, big.NewInt(9000000))
	if e != nil {
		return EmailFlow197{}, e
	}
	code := strconv.FormatInt(n.Int64()+1000000, 10)
	var sentAt time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&sentAt); e != nil {
		return EmailFlow197{}, e
	}
	if existing != "" {
		if _, e = tx.Exec(ctx, `UPDATE enrollment_challenges SET state='failed' WHERE id=$1 AND consumed_at IS NULL`, existing); e != nil {
			return EmailFlow197{}, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO enrollment_challenges(id,purpose,email_hash,source_hash,code_hash,target_merchant_id,target_auth_version,state,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,'sending',to_timestamp($8::numeric/1000000))`, challenge, purpose, s.digest("email", email), s.digest("source", source), s.digest("code", challenge, purpose, email, code), a.User.ID, a.User.AuthVersion, v.birth.Expires); e != nil {
		return EmailFlow197{}, e
	}
	if e = s.appendEmail197(ctx, tx, &v, "send-"+kind, emailFacts197{Email: email, Challenge: challenge, SendID: sendID, SentAt: sentAt.UnixMicro()}); e != nil {
		return EmailFlow197{}, e
	}
	if e = s.deadlineEmail197(ctx, tx, v, expiry); e != nil {
		return EmailFlow197{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return EmailFlow197{}, e
	}
	// Admission is durable before any network I/O. A process crash or lost DATA
	// reply leaves unknown/failed state; reading or retrying this flow never sends.
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	sendErr := delivery.SendVerification(sendCtx, email, purpose, code)
	cancel()
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer finishCancel()
	finish, e := s.pool.Begin(finishCtx)
	if e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	defer finish.Rollback(finishCtx)
	v, e = s.readEmailFlow197(finishCtx, finish, id, true)
	if e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	status, event := "sent", "sent-"+kind
	if sendErr != nil {
		status, event = "failed", "failed-"+kind
	}
	var alive bool
	if e = finish.QueryRow(finishCtx, `SELECT clock_timestamp()<to_timestamp($1::numeric/1000000)`, v.birth.Expires).Scan(&alive); e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	if !alive || v.phase != phase {
		status = "failed"
	} else if e = s.appendEmail197(finishCtx, finish, &v, event, emailFacts197{Challenge: challenge}); e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	if _, e = finish.Exec(finishCtx, `UPDATE enrollment_challenges SET state=$2 WHERE id=$1 AND state='sending'`, challenge, status); e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	if e = finish.Commit(finishCtx); e != nil {
		return EmailFlow197{}, ErrUnavailable
	}
	return v.public(), nil
}

func (s *Service) VerifyEmail197(ctx context.Context, a EmailActor197, id, code string, complete bool) (EmailFlow197, error) {
	if len(code) != 7 {
		return EmailFlow197{}, ErrCode
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return EmailFlow197{}, e
	}
	defer tx.Rollback(ctx)
	peek, source, e := s.preflightEmail197(ctx, tx, a, id)
	if e != nil {
		return EmailFlow197{}, e
	}
	purpose, email, challenge, phase := "contact-old", peek.birth.OriginalEmail, peek.oldChallenge, "old"
	if complete {
		purpose, email, challenge, phase = "contact-new", peek.email, peek.newChallenge, "new"
	}
	if peek.phase != phase || challenge == "" {
		return EmailFlow197{}, ErrConflict
	}
	if e = limit(ctx, tx, s.digest("verify-source", source), 900, 100); e != nil {
		return EmailFlow197{}, commitRejection(ctx, tx, e)
	}
	if e = limit(ctx, tx, s.digest("verify-email", email), 900, 30); e != nil {
		return EmailFlow197{}, commitRejection(ctx, tx, e)
	}
	v, expiry, e := s.ownEmailFlow197(ctx, tx, a, id)
	if e != nil {
		return EmailFlow197{}, e
	}
	if v.phase != phase {
		return EmailFlow197{}, ErrConflict
	}
	if _, e = s.verifyProof197(ctx, tx, purpose, challenge, email, source, code); e != nil {
		if errors.Is(e, ErrCode) || errors.Is(e, ErrLimited) {
			return EmailFlow197{}, commitRejection(ctx, tx, e)
		}
		return EmailFlow197{}, e
	}
	work, e := tx.Begin(ctx)
	if e != nil {
		return EmailFlow197{}, e
	}
	defer work.Rollback(ctx)
	if !complete {
		if e = s.appendEmail197(ctx, work, &v, "verify-old", emailFacts197{Challenge: challenge}); e != nil {
			return EmailFlow197{}, e
		}
	} else {
		facts := emailFacts197{Email: email, EmailHash: hex.EncodeToString(s.digest("email", email)), Challenge: challenge}
		if e = s.appendEmail197(ctx, work, &v, "complete", facts); e != nil {
			return EmailFlow197{}, e
		}
		if _, e = work.Exec(ctx, `SELECT set_config('goepay.email_binding197',$1,true)`, id); e != nil {
			return EmailFlow197{}, e
		}
		if v.birth.OriginalEmail == "" {
			_, e = work.Exec(ctx, `INSERT INTO enrollment_emails(email,email_hash,merchant_id,username,binding_flow197) VALUES($1,$2,$3,$4,$5)`, email, s.digest("email", email), a.User.ID, a.User.Username, id)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, e = work.Exec(ctx, `UPDATE enrollment_emails SET email=$2,email_hash=$3,verified_at=clock_timestamp(),binding_flow197=$6 WHERE merchant_id=$1 AND email=$4 AND username=$5`, a.User.ID, email, s.digest("email", email), v.birth.OriginalEmail, a.User.Username, id)
			if e == nil && tag.RowsAffected() != 1 {
				e = ErrConflict
			}
		}
		if e != nil {
			mapped := mapConflict(e)
			if errors.Is(mapped, ErrConflict) {
				return EmailFlow197{}, rejectExpiredWork(ctx, work, tx, mapped)
			}
			return EmailFlow197{}, e
		}
		if _, e = work.Exec(ctx, `UPDATE merchants SET profile=profile||jsonb_build_object('email',$2::text) WHERE id=$1`, a.User.ID, email); e != nil {
			return EmailFlow197{}, e
		}
		if _, e = work.Exec(ctx, `UPDATE console_users SET auth_version=auth_version+1 WHERE username=$1 AND role='merchant' AND subject_id=$2 AND auth_version=$3`, a.User.Username, a.User.ID, a.User.AuthVersion); e != nil {
			return EmailFlow197{}, e
		}
		if _, e = work.Exec(ctx, `DELETE FROM console_sessions WHERE role='merchant' AND subject_id=$1`, a.User.ID); e != nil {
			return EmailFlow197{}, e
		}
		if _, e = work.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'account',$1,'verified-email-change','{"fields":["email"]}')`, a.User.ID, a.User.Username); e != nil {
			return EmailFlow197{}, e
		}
	}
	// These checks follow every potentially blocking business write, including
	// self-revocation. The locked original expiry remains authoritative even
	// though successful completion intentionally removes the original session.
	if e = consumeProof(ctx, work, challenge); e != nil {
		if errors.Is(e, ErrCode) {
			return EmailFlow197{}, rejectExpiredWork(ctx, work, tx, e)
		}
		return EmailFlow197{}, e
	}
	if e = s.deadlineEmail197(ctx, work, v, expiry); e != nil {
		return EmailFlow197{}, rejectExpiredWork(ctx, work, tx, e)
	}
	if e = work.Commit(ctx); e != nil {
		return EmailFlow197{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return EmailFlow197{}, e
	}
	return v.public(), nil
}
