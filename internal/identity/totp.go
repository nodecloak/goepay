package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"time"
)

var ErrMFA = errors.New("invalid or reused authenticator code")

type MFA struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
	now  func() time.Time
}
type Enrollment struct {
	Secret  string    `json:"secret"`
	URI     string    `json:"otpauth_uri"`
	Expires time.Time `json:"expires_at"`
}

func NewMFA(pool *pgxpool.Pool, key []byte) (*MFA, error) {
	if len(key) != 32 {
		return nil, errors.New("MFA encryption key required")
	}
	derived := sha256.Sum256(append([]byte("goepay:mfa:v1:"), key...))
	block, err := aes.NewCipher(derived[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &MFA{pool, aead, time.Now}, nil
}
func (m *MFA) seal(secret []byte, username, role string) ([]byte, error) {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return m.aead.Seal(nonce, nonce, secret, []byte(username+"\x00"+role)), nil
}
func (m *MFA) open(raw []byte, username, role string) ([]byte, error) {
	n := m.aead.NonceSize()
	if len(raw) < n {
		return nil, ErrMFA
	}
	return m.aead.Open(nil, raw[:n], raw[n:], []byte(username+"\x00"+role))
}
func decodeTOTPSecret(secret string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
}
func totpCode(secret []byte, counter int64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(counter))
	h := hmac.New(sha1.New, secret)
	h.Write(b[:])
	digest := h.Sum(nil)
	offset := digest[len(digest)-1] & 15
	n := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func matchTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	for _, delta := range []int64{0, -1, 1} {
		counter := now.Unix()/30 + delta
		if counter >= 0 && hmac.Equal([]byte(code), []byte(totpCode(secret, counter))) {
			return counter, true
		}
	}
	return 0, false
}
func (m *MFA) Enabled(ctx context.Context, username, role string) (bool, error) {
	var enabled bool
	err := m.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_mfa WHERE username=$1 AND role=$2 AND secret IS NOT NULL)", username, role).Scan(&enabled)
	return enabled, err
}
func (m *MFA) Prepare(ctx context.Context, username, role string) (Enrollment, error) {
	var out Enrollment
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return out, err
	}
	encrypted, err := m.seal(secret, username, role)
	if err != nil {
		return out, err
	}
	expires := m.now().Add(10 * time.Minute)
	tag, err := m.pool.Exec(ctx, "INSERT INTO console_mfa(username,role,pending_secret,pending_until) VALUES($1,$2,$3,$4) ON CONFLICT(username,role) DO UPDATE SET pending_secret=EXCLUDED.pending_secret,pending_until=EXCLUDED.pending_until WHERE console_mfa.secret IS NULL", username, role, encrypted, expires)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() != 1 {
		return out, ErrMFA
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	query := url.Values{"secret": {encoded}, "issuer": {"GOEPAY"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return Enrollment{encoded, "otpauth://totp/" + url.PathEscape("GOEPAY:"+role+":"+username) + "?" + query.Encode(), expires}, nil
}
func (m *MFA) Confirm(ctx context.Context, username, role, code string) error {
	return m.consume(ctx, username, role, code, "confirm")
}
func (m *MFA) Verify(ctx context.Context, username, role, code string) error {
	return m.consume(ctx, username, role, code, "verify")
}
func (m *MFA) Disable(ctx context.Context, username, role, code string) error {
	return m.consume(ctx, username, role, code, "disable")
}
func (m *MFA) consume(ctx context.Context, username, role, code, action string) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if action != "verify" {
		var version int64
		if err = tx.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username=$1 AND role=$2 AND enabled FOR UPDATE", username, role).Scan(&version); err != nil {
			return ErrMFA
		}
	}
	var active, pending []byte
	var until *time.Time
	var last int64
	if err = tx.QueryRow(ctx, "SELECT secret,pending_secret,pending_until,last_counter FROM console_mfa WHERE username=$1 AND role=$2 FOR UPDATE", username, role).Scan(&active, &pending, &until, &last); err != nil {
		return ErrMFA
	}
	raw := active
	if action == "confirm" {
		if active != nil || until == nil || !m.now().Before(*until) {
			return ErrMFA
		}
		raw = pending
	}
	secret, err := m.open(raw, username, role)
	if err != nil {
		return ErrMFA
	}
	counter, ok := matchTOTP(secret, code, m.now())
	if !ok || counter <= last {
		return ErrMFA
	}
	switch action {
	case "confirm":
		_, err = tx.Exec(ctx, "UPDATE console_mfa SET secret=pending_secret,pending_secret=NULL,pending_until=NULL,last_counter=$3 WHERE username=$1 AND role=$2", username, role, counter)
	case "disable":
		_, err = tx.Exec(ctx, "UPDATE console_mfa SET secret=NULL,pending_secret=NULL,pending_until=NULL,last_counter=-1 WHERE username=$1 AND role=$2", username, role)
	default:
		_, err = tx.Exec(ctx, "UPDATE console_mfa SET last_counter=$3 WHERE username=$1 AND role=$2", username, role, counter)
	}
	if err != nil {
		return err
	}
	if action != "verify" {
		if _, err = tx.Exec(ctx, "UPDATE console_users SET auth_version=auth_version+1 WHERE username=$1 AND role=$2", username, role); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM console_sessions WHERE username=$1 AND role=$2", username, role); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation) SELECT role,subject_id,username,'mfa',subject_id,$3 FROM console_users WHERE username=$1 AND role=$2", username, role, action); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
