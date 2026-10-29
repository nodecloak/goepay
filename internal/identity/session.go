package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
	"strings"
	"sync"
	"time"
)

type User struct {
	AdminClass string `json:"admin_class,omitempty"`
	// Permissions is the raw console_users.permissions jsonb for an admin:
	// nil (SQL NULL) means an unrestricted operator, a JSON array of menu keys
	// means an explicit per-menu grant set. Not persisted in the session store;
	// it is re-read from the row on every request in the server dispatch.
	Permissions []byte `json:"-"`
	AuthVersion int64  `json:"-"`
	Username    string `json:"-"`
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
}
type Session struct {
	User    User
	CSRF    string
	Expires time.Time
	Issuer  *SessionIssuer
}
type Sessions struct {
	pool   *pgxpool.Pool
	mu     sync.Mutex
	items  map[string]Session
	now    func() time.Time
	Secure bool
}

func NewSessions(secure bool) *Sessions {
	return &Sessions{items: make(map[string]Session), now: time.Now, Secure: secure}
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func (s *Sessions) Create(u User) (string, string, error) {
	if s.pool != nil {
		return s.createPersistent(u)
	}
	token, err := randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.items {
		if !s.now().Before(v.Expires) {
			delete(s.items, k)
		}
	}
	s.items[token] = Session{User: u, CSRF: csrf, Expires: s.now().Add(12 * time.Hour)}
	return token, csrf, nil
}
func (s *Sessions) Get(token, role string) (Session, bool) {
	if s.pool != nil {
		return s.getPersistent(token, role)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[token]
	if !ok {
		return Session{}, false
	}
	if !s.now().Before(v.Expires) {
		delete(s.items, token)
		return Session{}, false
	}
	return v, subtle.ConstantTimeCompare([]byte(v.User.Role), []byte(role)) == 1
}
func (s *Sessions) Delete(token string) {
	if s.pool != nil {
		s.deletePersistent(token)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, token)
}
func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", fmt.Errorf("password length must be 12..1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return "$argon2id$v=19$m=65536,t=2,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}
func CheckPassword(encoded, password string) bool {
	if len(password) > 1024 {
		return false
	}
	p := strings.Split(encoded, "$")
	if len(p) != 6 || p[1] != "argon2id" || p[2] != "v=19" || p[3] != "m=65536,t=2,p=2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(p[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(p[5])
	if e != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}
