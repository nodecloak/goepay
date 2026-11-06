package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"goepay/internal/enrollment"
	"goepay/internal/mailaccount"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEmail197ActualBrowserHTTPPrivateTLSFirstBindingAndReauthentication(t *testing.T) {
	email197Browser197(t, false)
}
func TestEmail197ActualBrowserHTTPPrivateTLSExistingBindingAndReauthentication(t *testing.T) {
	email197Browser197(t, true)
}
func email197Browser197(t *testing.T, existing bool) {
	if os.Getenv("GOEPAY_EMAIL197_BROWSER") != "1" {
		t.Skip("explicit local Playwright required")
	}
	for _, width := range []int{375, 1440} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			f := email197Local(t)
			mid := int64(1)
			mode := "first"
			submissions := int32(1)
			if existing {
				mid, _ = f.legacy(t)
				mode = "existing"
				submissions = 2
			}
			peer := mail195LocalTLS(t, false, "old197@example.test")
			master := make([]byte, 32)
			key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
			f.s.CheckoutKey = key[:]
			state := mail195Data(t, mail195Request(f.channel168Fixture, "GET", "/smtp", ""))
			mail195SaveTLS(t, f.channel168Fixture, state, peer)
			resolver := &mailaccount.Verification{Store: mailaccount.FromMaster(f.p, master), RootCAs: peer.roots}
			service, e := enrollment.New(f.p, f.s.Store, master, resolver)
			if e != nil {
				t.Fatal(e)
			}
			f.s.Enrollment = enrollment.NewHandler(service, "http://127.0.0.1:8187")
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/__email197/code" {
					if r.Method != "GET" || r.Header.Get("X-Synthetic-Fixture") != f.token {
						w.WriteHeader(404)
						return
					}
					peer.mu.Lock()
					defer peer.mu.Unlock()
					if len(peer.messages) == 0 {
						w.WriteHeader(404)
						return
					}
					msg, e := mail.ReadMessage(strings.NewReader(peer.messages[len(peer.messages)-1]))
					if e != nil {
						w.WriteHeader(500)
						return
					}
					raw, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
					if e != nil {
						w.WriteHeader(500)
						return
					}
					code := regexp.MustCompile(`[1-9][0-9]{6}`).FindString(string(raw))
					w.Header().Set("Cache-Control", "no-store")
					json.NewEncoder(w).Encode(map[string]string{"code": code})
					return
				}
				f.s.Handler().ServeHTTP(w, r)
			}))
			t.Cleanup(api.Close)
			root, e := filepath.Abs(filepath.Join("..", ".."))
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "web", "e2e", "email197-http-browser.mjs"))
			cmd.Dir = filepath.Join(root, "web")
			cmd.Env = append(os.Environ(), "EMAIL197_API="+api.URL, "EMAIL197_COOKIE="+f.token, "EMAIL197_MODE="+mode, "EMAIL198_UID="+strconv.FormatInt(mid, 10), "EMAIL197_WIDTH="+strconv.Itoa(width), "EMAIL197_ARTIFACTS="+filepath.Join(root, ".tools", "email197-actual-browser"))
			output, e := cmd.CombinedOutput()
			if e != nil {
				if len(output) > 3500 {
					output = output[len(output)-3500:]
				}
				t.Fatalf("actual email197 browser %v\n%s", e, output)
			}
			var email string
			var sessions, effects int
			if e = f.p.QueryRow(t.Context(), `SELECT email FROM enrollment_emails WHERE merchant_id=$1`, mid).Scan(&email); e != nil || email != "recipient195@example.test" {
				t.Fatal("actual SMTP proof did not bind recovery identity", email, e)
			}
			if e = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM console_sessions WHERE role='merchant' AND subject_id=$1),(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`, mid).Scan(&sessions, &effects); e != nil || sessions != 0 || effects != 0 || peer.connections.Load() != submissions || peer.data.Load() != submissions {
				t.Fatal("browser repeated send, retained session or touched funds", sessions, effects, peer.connections.Load(), peer.data.Load(), e)
			}
		})
	}
}
