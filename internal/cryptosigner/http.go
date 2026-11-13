package cryptosigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"goepay/internal/cryptochain"
)

// NewHandler requires a CA-verified client and an exact DNS/URI SAN allowlist.
// The TLS listener must additionally use RequireAndVerifyClientCert.
func NewHandler(s *Service, allowedSANs []string) http.Handler {
	allowed := map[string]bool{}
	for _, name := range allowedSANs {
		if name != "" {
			allowed[name] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "verified client certificate required", http.StatusUnauthorized)
			return
		}
		cert := r.TLS.PeerCertificates[0]
		authorized := false
		for _, name := range cert.DNSNames {
			if allowed[name] {
				authorized = true
			}
		}
		for _, uri := range cert.URIs {
			if allowed[uri.String()] {
				authorized = true
			}
		}
		if !authorized {
			http.Error(w, "client identity denied", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/sign" && r.URL.Path != "/v1/cancel" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var p cryptochain.PreparedTransaction
		if err := dec.Decode(&p); err != nil {
			http.Error(w, "invalid sign request", http.StatusBadRequest)
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			http.Error(w, "trailing request data", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/v1/cancel" {
			receipt, err := s.Cancel(r.Context(), p)
			if err != nil {
				writeSignerError(w, err)
				return
			}
			json.NewEncoder(w).Encode(cancellationResponse{RequestID: receipt.requestID, Binding: receipt.binding, State: "cancelled"})
			return
		}
		signed, err := s.Sign(r.Context(), p)
		if err != nil {
			writeSignerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(signed)
	})
}

func writeSignerError(w http.ResponseWriter, err error) {
	code, status := "unavailable", http.StatusServiceUnavailable
	switch {
	case errors.Is(err, ErrPolicy):
		code, status = "policy_denied", http.StatusForbidden
	case errors.Is(err, ErrRequestConflict):
		code, status = "request_conflict", http.StatusConflict
	case errors.Is(err, ErrIndeterminate):
		code, status = "indeterminate", http.StatusConflict
	case errors.Is(err, ErrCancelled):
		code, status = "cancelled", http.StatusConflict
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code})
}

type cancellationResponse struct {
	RequestID string `json:"request_id"`
	Binding   string `json:"binding"`
	State     string `json:"state"`
}

type Client struct {
	endpoint string
	http     *http.Client
}

func NewClient(endpoint string, tlsConfig *tls.Config) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || tlsConfig == nil || tlsConfig.InsecureSkipVerify || tlsConfig.RootCAs == nil || len(tlsConfig.Certificates) == 0 {
		return nil, fmt.Errorf("signer client requires HTTPS, trusted CA and client certificate")
	}
	cfg := tlsConfig.Clone()
	cfg.MinVersion = tls.VersionTLS13
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// NewMTLSClient is used by the API process; its key is a TLS identity key, not
// a treasury signing key. The treasury key never leaves the signer process.
func NewMTLSClient(endpoint, certFile, keyFile, caFile string) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	ca, err := readCAs(caFile)
	if err != nil {
		return nil, err
	}
	return NewClient(endpoint, &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: ca, MinVersion: tls.VersionTLS13})
}

func ServerTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	ca, err := readCAs(caFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: ca, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}, nil
}
func readCAs(path string) (*x509.CertPool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("invalid signer CA bundle")
	}
	return pool, nil
}

func (c *Client) Sign(ctx context.Context, p cryptochain.PreparedTransaction) (cryptochain.SignedTransaction, error) {
	var result cryptochain.SignedTransaction
	b, err := c.post(ctx, "/v1/sign", p)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(b, &result); err != nil || result.Chain != p.Chain || result.TxID == "" || result.RawTransaction == "" {
		return cryptochain.SignedTransaction{}, fmt.Errorf("%w: invalid signer response", ErrIndeterminate)
	}
	if err = VerifySigned(p, result); err != nil {
		return cryptochain.SignedTransaction{}, fmt.Errorf("%w: signer response validation: %v", ErrIndeterminate, err)
	}
	return result, nil
}

// Cancel obtains a process-local proof only from this client's authenticated
// signer endpoint and only after checking the complete prepared binding. A
// timeout or an arbitrary JSON receipt can never authorize release of funds.
func (c *Client) Cancel(ctx context.Context, p cryptochain.PreparedTransaction) (CancellationReceipt, error) {
	var zero CancellationReceipt
	binding, err := preparedBinding(p)
	if err != nil {
		return zero, err
	}
	b, err := c.post(ctx, "/v1/cancel", p)
	if err != nil {
		return zero, err
	}
	var response cancellationResponse
	if err = json.Unmarshal(b, &response); err != nil || response.State != "cancelled" || response.RequestID != p.RequestID || response.Binding != binding {
		return zero, fmt.Errorf("%w: invalid cancellation response", ErrIndeterminate)
	}
	return CancellationReceipt{requestID: p.RequestID, binding: binding}, nil
}

func (c *Client) post(ctx context.Context, path string, p cryptochain.PreparedTransaction) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: signer transport: %v", ErrIndeterminate, err)
	}
	defer resp.Body.Close()
	b, err = io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: signer response: %v", ErrIndeterminate, err)
	}
	if len(b) > 2<<20 {
		return nil, fmt.Errorf("%w: oversized signer response", ErrIndeterminate)
	}
	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Error string `json:"error"`
		}
		json.Unmarshal(b, &problem)
		switch problem.Error {
		case "policy_denied":
			return nil, ErrPolicy
		case "request_conflict":
			return nil, ErrRequestConflict
		case "cancelled":
			return nil, ErrCancelled
		default:
			return nil, ErrIndeterminate
		}
	}
	return b, nil
}
