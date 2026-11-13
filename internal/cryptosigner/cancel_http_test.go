package cryptosigner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMTLSCancellationAuthenticatesProofAndFencesSigning(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root, serverCert, clientCert, foreignCert := testCertificates(t)
	server := httptest.NewUnstartedServer(NewHandler(s, []string{"payments-api"}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCert}, ClientCAs: root, ClientAuth: tls.RequireAndVerifyClientCert}
	server.StartTLS()
	defer server.Close()
	client, err := NewClient(server.URL, &tls.Config{RootCAs: root, Certificates: []tls.Certificate{clientCert}})
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("cancel-over-mtls", 0, "100000")
	proof, err := client.Cancel(t.Context(), p)
	if err != nil || !VerifyCancellation(p, proof) {
		t.Fatalf("mTLS cancellation failed: %v", err)
	}
	if _, err = client.Sign(t.Context(), p); !errors.Is(err, ErrCancelled) {
		t.Fatalf("sign client lost cancellation fence: %v", err)
	}
	foreign, err := NewClient(server.URL, &tls.Config{RootCAs: root, Certificates: []tls.Certificate{foreignCert}})
	if err != nil {
		t.Fatal(err)
	}
	other := ethereumPrepared("unauthorized-cancel", 0, "100000")
	badProof, err := foreign.Cancel(t.Context(), other)
	if err == nil || VerifyCancellation(other, badProof) {
		t.Fatalf("foreign client minted proof: %v", err)
	}
	if _, err = client.Sign(t.Context(), other); err != nil {
		t.Fatalf("foreign cancellation affected valid payment: %v", err)
	}
	if badProof, err = client.Cancel(t.Context(), other); !errors.Is(err, ErrIndeterminate) || VerifyCancellation(other, badProof) {
		t.Fatalf("signed request cancelled over HTTP: %v", err)
	}
	unauth := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: root, MinVersion: tls.VersionTLS13}}}
	if resp, err := unauth.Post(server.URL+"/v1/cancel", "application/json", nil); err == nil {
		resp.Body.Close()
		t.Fatal("cancel accepted without client certificate")
	}
}

func TestCancellationClientRejectsUnboundAndUnknownResponses(t *testing.T) {
	for _, mode := range []string{"wrong-binding", "wrong-id", "unknown-state", "lost-response"} {
		t.Run(mode, func(t *testing.T) {
			root, serverCert, clientCert, _ := testCertificates(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/cancel" {
					t.Errorf("wrong cancellation route: %s", r.URL.Path)
				}
				var p struct {
					RequestID string `json:"request_id"`
				}
				json.NewDecoder(r.Body).Decode(&p)
				if mode == "lost-response" {
					h, ok := w.(http.Hijacker)
					if !ok {
						t.Error("cannot simulate lost response")
						return
					}
					conn, _, err := h.Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				request := ethereumPrepared(p.RequestID, 0, "100000")
				binding, _ := preparedBinding(request)
				id, state := p.RequestID, "cancelled"
				if mode == "wrong-binding" {
					binding = "wrong"
				}
				if mode == "wrong-id" {
					id = "another-request"
				}
				if mode == "unknown-state" {
					state = "pending"
				}
				json.NewEncoder(w).Encode(map[string]string{"request_id": id, "binding": binding, "state": state})
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCert}, ClientCAs: root, ClientAuth: tls.RequireAndVerifyClientCert}
			server.StartTLS()
			defer server.Close()
			client, err := NewClient(server.URL, &tls.Config{RootCAs: root, Certificates: []tls.Certificate{clientCert}})
			if err != nil {
				t.Fatal(err)
			}
			p := ethereumPrepared("untrusted-reply", 0, "100000")
			proof, err := client.Cancel(context.Background(), p)
			if !errors.Is(err, ErrIndeterminate) || VerifyCancellation(p, proof) {
				t.Fatalf("unbound or unknown proof accepted: %v", err)
			}
		})
	}
}

func TestCancellationHandlerRejectsPlainHTTP(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := httptest.NewRecorder()
	NewHandler(s, []string{"payments-api"}).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/cancel", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("plain cancellation accepted: %d", w.Code)
	}
}

func TestPersistedCancellationWithLostResponseRecoversOnlyOriginalProof(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	s, err := NewService(cfg, testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root, serverCert, clientCert, _ := testCertificates(t)
	handler := NewHandler(s, []string{"payments-api"})
	var dropped atomic.Bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/cancel" && !dropped.Swap(true) {
			recorded := httptest.NewRecorder()
			handler.ServeHTTP(recorded, r)
			if recorded.Code != http.StatusOK {
				t.Errorf("cancellation setup failed: %d", recorded.Code)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCert}, ClientCAs: root, ClientAuth: tls.RequireAndVerifyClientCert}
	server.StartTLS()
	defer server.Close()
	client, err := NewClient(server.URL, &tls.Config{RootCAs: root, Certificates: []tls.Certificate{clientCert}})
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("lost-cancel-reply", 0, "100000")
	proof, err := client.Cancel(t.Context(), p)
	if !errors.Is(err, ErrIndeterminate) || VerifyCancellation(p, proof) {
		t.Fatalf("lost response created authority: %v", err)
	}
	if _, err = s.Sign(t.Context(), p); !errors.Is(err, ErrCancelled) {
		t.Fatalf("durable fence was lost: %v", err)
	}
	proof, err = client.Cancel(t.Context(), p)
	if err != nil || !VerifyCancellation(p, proof) {
		t.Fatalf("original proof not recoverable: %v", err)
	}
	raw, err := os.ReadFile(cfg.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "\n") != 1 {
		t.Fatal("cancellation retry appended another state transition")
	}
}
