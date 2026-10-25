package identity

import (
	"testing"
	"time"
)

func TestSessionsExpireAndCannotCrossRole(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewSessions(false)
	s.now = func() time.Time { return now }
	token, csrf, err := s.Create(User{ID: 7, Name: "m", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	if csrf == "" || csrf == token {
		t.Fatal("missing independent CSRF")
	}
	if _, ok := s.Get(token, "admin"); ok {
		t.Fatal("role confusion")
	}
	if _, ok := s.Get(token, "merchant"); !ok {
		t.Fatal("session missing")
	}
	now = now.Add(13 * time.Hour)
	if _, ok := s.Get(token, "merchant"); ok {
		t.Fatal("expired session accepted")
	}
}
func TestRevocation(t *testing.T) {
	s := NewSessions(false)
	token, _, _ := s.Create(User{ID: 1, Role: "admin"})
	s.Delete(token)
	if _, ok := s.Get(token, "admin"); ok {
		t.Fatal("revoked accepted")
	}
}
func TestPasswords(t *testing.T) {
	h, err := HashPassword("a long test password")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "a long test password") || CheckPassword(h, "wrong") {
		t.Fatal("password verification")
	}
	h2, _ := HashPassword("a long test password")
	if h == h2 {
		t.Fatal("salt reused")
	}
}
