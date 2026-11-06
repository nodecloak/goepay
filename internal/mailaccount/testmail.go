package mailaccount

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/mailer"
	"goepay/internal/siteinfo"
)

var ErrTestMissing = errors.New("test mail request not found")
var ErrTestLimited = errors.New("test mail submission limit exceeded")
var testID195 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type TestResult struct {
	ID     string `json:"request_id"`
	Status string `json:"status"`
}
type TestInput struct {
	ID       string
	To       string
	Expected string
}
type testFacts struct {
	Version            int
	ID                 string
	ActorID            int64
	ActorName          string
	RecipientHash      []byte
	AccountFingerprint string
	AccountProof       []byte
	CreatedAt          time.Time
}
type testClaim struct {
	facts testFacts
	proof []byte
}
type testAccepted struct {
	Version      int
	ID           string
	RequestProof []byte
	AcceptedAt   time.Time
}

func testMailbox(to string) bool {
	if len(to) == 0 || len(to) > 254 {
		return false
	}
	a, e := mail.ParseAddress(to)
	if e != nil || a.Name != "" || a.Address != to {
		return false
	}
	for _, r := range to {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
func decodeTestFacts(raw []byte, out any) error {
	if len(raw) > 8192 {
		return ErrSource
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrSource
	}
	return nil
}
func (s *Store) readTest(ctx context.Context, tx pgx.Tx, id string) (testClaim, error) {
	var c testClaim
	var raw []byte
	var actor int64
	var at time.Time
	e := tx.QueryRow(ctx, `SELECT facts,proof_hash,actor_id,created_at FROM mail_test_requests195 WHERE request_id=$1`, id).Scan(&raw, &c.proof, &actor, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, ErrTestMissing
	}
	if e != nil {
		return c, ErrSource
	}
	if decodeTestFacts(raw, &c.facts) != nil {
		return c, ErrSource
	}
	want, e := s.digest("test-request", c.facts)
	if e != nil || !hmac.Equal(want, c.proof) || c.facts.Version != 1 || c.facts.ID != id || c.facts.ActorID != actor || actor <= 0 || c.facts.ActorName == "" || len(c.facts.RecipientHash) != 32 || len(c.facts.AccountProof) != 32 || len(c.facts.AccountFingerprint) != 64 || !c.facts.CreatedAt.Equal(at) {
		return c, ErrSource
	}
	var witnesses int
	if e = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource='mail-test195' AND actor_id=$1 AND changes->>'request_id'=$2 AND changes->>'commitment'=$3`, actor, id, hex.EncodeToString(c.proof)).Scan(&witnesses); e != nil || witnesses != 1 {
		return c, ErrSource
	}
	return c, nil
}
func (s *Store) testResult(ctx context.Context, tx pgx.Tx, c testClaim) (TestResult, error) {
	out := TestResult{ID: c.facts.ID, Status: "unknown"}
	var raw, proof, request []byte
	e := tx.QueryRow(ctx, `SELECT facts,proof_hash,request_proof FROM mail_test_outcomes195 WHERE request_id=$1`, c.facts.ID).Scan(&raw, &proof, &request)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, ErrSource
	}
	var f testAccepted
	if decodeTestFacts(raw, &f) != nil {
		return out, ErrSource
	}
	expected, e := s.digest("test-accepted", f)
	if e != nil || !hmac.Equal(expected, proof) || !hmac.Equal(request, c.proof) || !hmac.Equal(f.RequestProof, c.proof) || f.Version != 1 || f.ID != c.facts.ID || f.AcceptedAt.IsZero() || f.AcceptedAt.Before(c.facts.CreatedAt) {
		return out, ErrSource
	}
	out.Status = "accepted"
	return out, nil
}
func (s *Store) ReadTest(ctx context.Context, a Actor, id string) (TestResult, error) {
	out := TestResult{ID: id, Status: "unknown"}
	if !testID195.MatchString(id) {
		return out, ErrInvalid
	}
	if s == nil || s.pool == nil {
		return out, ErrSource
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, ErrSource
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	claim, e := s.readTest(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if claim.facts.ActorID != a.User.ID || claim.facts.ActorName != a.User.Username {
		return out, ErrTestMissing
	}
	out, e = s.testResult(ctx, tx, claim)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) SubmitTest(ctx context.Context, a Actor, in TestInput, roots *x509.CertPool) (TestResult, error) {
	out := TestResult{ID: in.ID, Status: "unknown"}
	if !testID195.MatchString(in.ID) || !testMailbox(in.To) || len(in.Expected) != 64 {
		return out, ErrInvalid
	}
	if s == nil || s.pool == nil {
		return out, ErrSource
	}
	recipient, e := s.digest("test-recipient", in.To)
	if e != nil {
		return out, ErrSource
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, ErrSource
	}
	defer tx.Rollback(ctx)
	account, e := s.load(ctx, tx, false)
	if e != nil {
		return out, ErrSource
	}
	// Serialize declarations, including new IDs, so persisted actor/global
	// budgets cannot be bypassed by simultaneous original confirmations.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(7170195002)); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	old, e := s.readTest(ctx, tx, in.ID)
	if e == nil {
		if old.facts.ActorID != a.User.ID || old.facts.ActorName != a.User.Username {
			return out, ErrTestMissing
		}
		if old.facts.AccountFingerprint != in.Expected || !hmac.Equal(old.facts.RecipientHash, recipient) {
			return out, ErrConflict
		}
		out, e = s.testResult(ctx, tx, old)
		if e != nil {
			return out, e
		}
		if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
			return out, e
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, ErrTestMissing) {
		return out, e
	}
	var orphan bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='mail-test195'AND changes->>'request_id'=$1)`, in.ID).Scan(&orphan); e != nil || orphan {
		return out, ErrSource
	}
	if !account.managed || account.facts.Status != 1 {
		return out, ErrSource
	}
	current, e := s.view(account)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(current.Fingerprint), []byte(in.Expected)) {
		return out, ErrConflict
	}
	var actorCount, total int
	if e = tx.QueryRow(ctx, `SELECT count(*)FILTER(WHERE actor_id=$1),count(*)FROM mail_test_requests195 WHERE created_at>clock_timestamp()-interval '10 minutes'`, a.User.ID).Scan(&actorCount, &total); e != nil {
		return out, ErrSource
	}
	if actorCount >= 5 || total >= 20 {
		return out, ErrTestLimited
	}
	cfg, e := SMTPConfig(account.config)
	if e != nil {
		return out, e
	}
	site, e := siteinfo.Load(ctx, tx)
	if e != nil {
		return out, ErrSource
	}
	cfg.FromName = site.Name
	cfg.ReplyTo = cfg.From
	cfg.RootCAs = roots
	sender, e := mailer.NewSMTP(cfg)
	if e != nil {
		return out, ErrSource
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return out, e
	}
	f := testFacts{Version: 1, ID: in.ID, ActorID: a.User.ID, ActorName: a.User.Username, RecipientHash: recipient, AccountFingerprint: in.Expected, AccountProof: account.proof, CreatedAt: at.UTC()}
	proof, e := s.digest("test-request", f)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mail_test_requests195(request_id,actor_id,created_at,facts,proof_hash)VALUES($1,$2,$3,$4::jsonb,$5)`, in.ID, a.User.ID, f.CreatedAt, string(raw), proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'mail-test195',0,'mail-test-submit',jsonb_build_object('request_id',$3::text,'commitment',$4::text))`, a.User.ID, a.User.Username, in.ID, hex.EncodeToString(proof)); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	// The permanently committed declaration precedes all SMTP I/O. Unknown
	// replies, process interruption and duplicate requests can never resend it.
	delivery, cancel := context.WithTimeout(ctx, 5*time.Second)
	e = sender.SendTestEmail(delivery, in.To)
	cancel()
	if e != nil {
		return out, nil
	}
	if s.acceptTest(testClaim{facts: f, proof: proof}) == nil {
		out.Status = "accepted"
	}
	return out, nil
}
func (s *Store) acceptTest(claim testClaim) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	original, e := s.readTest(ctx, tx, claim.facts.ID)
	if e != nil || !hmac.Equal(original.proof, claim.proof) {
		return ErrSource
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return e
	}
	f := testAccepted{Version: 1, ID: claim.facts.ID, RequestProof: claim.proof, AcceptedAt: at.UTC()}
	proof, e := s.digest("test-accepted", f)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO mail_test_outcomes195(request_id,request_proof,facts,proof_hash)VALUES($1,$2,$3::jsonb,$4)ON CONFLICT(request_id)DO NOTHING`, f.ID, claim.proof, string(raw), proof); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
