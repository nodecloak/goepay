package noticeconfig231

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"time"
)

func qqKey(secret string) ed25519.PrivateKey {
	if secret == "" {
		return nil
	}
	seed := []byte(secret)
	for len(seed) < ed25519.SeedSize {
		seed = append(seed, seed...)
	}
	return ed25519.NewKeyFromSeed(seed[:ed25519.SeedSize])
}
func verifyQQ(secret, stamp, sig string, body []byte, now time.Time) bool {
	if secret == "" || len(stamp) > 20 {
		return false
	}
	n, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || strconv.FormatInt(n, 10) != stamp || n < now.Unix()-300 || n > now.Unix()+300 {
		return false
	}
	b, e := hex.DecodeString(sig)
	return e == nil && len(b) == ed25519.SignatureSize && ed25519.Verify(qqKey(secret).Public().(ed25519.PublicKey), append([]byte(stamp), body...), b)
}

type Attempt struct {
	ID        string    `json:"id"`
	Challenge string    `json:"challenge"`
	Expires   time.Time `json:"expires_at"`
}

func (Attempt) String() string   { return "notice231.Attempt(redacted)" }
func (Attempt) GoString() string { return "notice231.Attempt(redacted)" }
func randomID() (string, error) {
	b := make([]byte, 24)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func (s *Store) StartBinding(ctx context.Context, a Actor, revision int64) (Attempt, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Attempt{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('notice231/qqbot',0))"); e != nil {
		return Attempt{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return Attempt{}, e
	}
	v, c, _, e := s.read(ctx, tx, "qqbot")
	if e != nil {
		return Attempt{}, e
	}
	if v.Revision != revision {
		return Attempt{}, ErrConflict
	}
	if c.AppID == "" || !v.Enabled {
		return Attempt{}, ErrInvalid
	}
	var active bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM qq_binding_attempts231 WHERE revision=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp())", revision).Scan(&active); e != nil {
		return Attempt{}, e
	}
	if active {
		return Attempt{}, ErrConflict
	}
	id, e := randomID()
	if e != nil {
		return Attempt{}, e
	}
	code, e := randomID()
	if e != nil {
		return Attempt{}, e
	}
	out := Attempt{ID: id, Challenge: "GOEPAY-" + code[:20]}
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()+interval '5 minutes'").Scan(&out.Expires); e != nil {
		return Attempt{}, e
	}
	session := sha256.Sum256([]byte(a.Token))
	challenge := s.digest("challenge", out.Challenge)
	proof := s.digest("attempt", id, revision, session[:], a.User.Username, challenge, out.Expires.UTC().Format(time.RFC3339Nano))
	if _, e = tx.Exec(ctx, "INSERT INTO qq_binding_attempts231(id,revision,session_hash,username,challenge_hash,expires_at,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7)", id, revision, session[:], a.User.Username, challenge, out.Expires, proof); e != nil {
		return Attempt{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return Attempt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Attempt{}, e
	}
	return out, nil
}
func (s *Store) CancelBinding(ctx context.Context, a Actor, id string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.actor(ctx, tx, a); e != nil {
		return e
	}
	d := sha256.Sum256([]byte(a.Token))
	var proof []byte
	e = tx.QueryRow(ctx, "SELECT proof_hash FROM qq_binding_attempts231 WHERE id=$1 AND session_hash=$2 AND consumed_at IS NULL FOR UPDATE", id, d[:]).Scan(&proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	changes, _ := json.Marshal(map[string]string{"attempt_hash": hex.EncodeToString(s.digest("cancel", id, proof))})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'qq-binding231',0,'cancel',$3)`, a.User.ID, a.User.Username, changes); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE qq_binding_attempts231 SET consumed_at=clock_timestamp() WHERE id=$1 AND session_hash=$2 AND consumed_at IS NULL", id, d[:]); e != nil {
		return e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) Unbind(ctx context.Context, a Actor, revision int64) (View, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('notice231/qqbot',0))"); e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	v, c, _, e := s.read(ctx, tx, "qqbot")
	if e != nil {
		return View{}, e
	}
	if v.Revision != revision {
		return View{}, ErrConflict
	}
	c.Recipient = ""
	out, e := s.write(ctx, tx, a, "qqbot", v, c, v.Enabled, "unbind")
	if e != nil {
		return View{}, e
	}
	if e = s.actor(ctx, tx, a); e != nil {
		return View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return View{}, e
	}
	return out, nil
}

// QQCallback validates exactly the signed raw bytes. A normal message never
// binds automatically: it must consume an explicit, original-session challenge.
// It sends no outbound reply, so a lost callback response cannot duplicate one.
func (s *Store) QQCallback(ctx context.Context, appid, stamp, sig string, raw []byte, now time.Time) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, ErrInvalid
	}
	var event struct {
		Op   int    `json:"op"`
		Type string `json:"t"`
		ID   string `json:"id"`
		Data struct {
			Token   string `json:"plain_token"`
			EventTS string `json:"event_ts"`
			Content string `json:"content"`
			Author  struct {
				OpenID string `json:"user_openid"`
			} `json:"author"`
		} `json:"d"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return nil, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('notice231/qqbot',0))"); e != nil {
		return nil, e
	}
	v, c, managed, e := s.read(ctx, tx, "qqbot")
	if e != nil {
		return nil, e
	}
	if !managed || !v.Enabled || c.Secret == "" || (appid != "" && appid != c.AppID) {
		return nil, ErrInvalid
	}
	if event.Op == 13 {
		// This unsigned signing endpoint must never sign JSON that could be replayed
		// as a normal event body under the provider's shared Ed25519 key.
		if !validQQChallenge231(event.Data.Token, event.Data.EventTS, now) {
			return nil, ErrInvalid
		}
		return map[string]string{"plain_token": event.Data.Token, "signature": hex.EncodeToString(ed25519.Sign(qqKey(c.Secret), []byte(event.Data.EventTS+event.Data.Token)))}, nil
	}
	if !verifyQQ(c.Secret, stamp, sig, raw, now) {
		return nil, ErrInvalid
	}
	if event.Op != 0 || event.Type != "C2C_MESSAGE_CREATE" {
		return map[string]string{}, nil
	}
	code := strings.TrimSpace(event.Data.Content)
	if !botID.MatchString(event.Data.Author.OpenID) || !strings.HasPrefix(code, "GOEPAY-") || len(code) != 27 {
		return map[string]string{}, nil
	}
	var id, username string
	var session, challenge, proof []byte
	var expires time.Time
	e = tx.QueryRow(ctx, "SELECT id,username,session_hash,challenge_hash,expires_at,proof_hash FROM qq_binding_attempts231 WHERE revision=$1 AND challenge_hash=$2 AND consumed_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE", v.Revision, s.digest("challenge", code)).Scan(&id, &username, &session, &challenge, &expires, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if e != nil {
		return nil, e
	}
	if !hmac.Equal(proof, s.digest("attempt", id, v.Revision, session, username, challenge, expires.UTC().Format(time.RFC3339Nano))) {
		return nil, ErrConflict
	}
	var cancelled bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='qq-binding231' AND operation='cancel' AND changes->>'attempt_hash'=$1)", hex.EncodeToString(s.digest("cancel", id, proof))).Scan(&cancelled); e != nil {
		return nil, e
	}
	if cancelled {
		return map[string]string{}, nil
	}
	var actor Actor
	// Same row/session locks as the interactive authorization. Neither revocation
	// nor a credential rotation may race the one-shot binding commit.
	e = tx.QueryRow(ctx, `SELECT u.subject_id,u.auth_version,u.username FROM console_users u JOIN console_sessions s ON s.username=u.username AND s.role=u.role AND s.subject_id=u.subject_id AND s.auth_version=u.auth_version WHERE s.token_hash=$1 AND u.username=$2 AND u.role='admin' AND u.enabled AND u.admin_class='super' AND s.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM console_session_issuers i WHERE i.token_hash=s.token_hash) FOR SHARE OF u,s`, session, username).Scan(&actor.User.ID, &actor.User.AuthVersion, &actor.User.Username)
	if errors.Is(e, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if e != nil {
		return nil, e
	}
	c.Recipient = event.Data.Author.OpenID
	if _, e = s.write(ctx, tx, actor, "qqbot", v, c, v.Enabled, "bind"); e != nil {
		return nil, e
	}
	hash := s.digest("callback", raw)
	if _, e = tx.Exec(ctx, "UPDATE qq_binding_attempts231 SET consumed_at=clock_timestamp(),event_hash=$2 WHERE id=$1 AND consumed_at IS NULL", id, hash); e != nil {
		return nil, e
	}
	var alive bool
	if e = tx.QueryRow(ctx, "SELECT expires_at>clock_timestamp() FROM console_sessions WHERE token_hash=$1", session).Scan(&alive); e != nil || !alive {
		return nil, ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return map[string]string{}, nil
}
func validQQChallenge231(token, stamp string, now time.Time) bool {
	if !botID.MatchString(token) {
		return false
	}
	n, e := strconv.ParseInt(stamp, 10, 64)
	return e == nil && strconv.FormatInt(n, 10) == stamp && n >= now.Unix()-300 && n <= now.Unix()+300
}
