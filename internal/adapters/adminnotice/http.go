package adminnotice

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/platform/httpclient"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

func newHTTPClient(client *http.Client) (*http.Client, error) {
	if client == nil {
		client = httpclient.New()
	}
	if transport, ok := client.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		return nil, ErrDelivery
	}
	copy := *client
	copy.Jar = nil
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Timeout <= 0 || copy.Timeout > 10*time.Second {
		copy.Timeout = 10 * time.Second
	}
	return &copy, nil
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any) (map[string]json.RawMessage, error) {
	raw, e := json.Marshal(payload)
	if e != nil || len(raw) > 16384 {
		return nil, ErrDelivery
	}
	return requestJSON(ctx, client, endpoint, raw, "application/json", "")
}

func requestJSON(ctx context.Context, client *http.Client, endpoint string, raw []byte, contentType, authorization string) (map[string]json.RawMessage, error) {
	if ctx == nil || ctx.Err() != nil || client == nil || len(raw) > 16384 {
		return nil, ErrDelivery
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(work, "POST", endpoint, bytes.NewReader(raw))
	if e != nil {
		return nil, ErrDelivery
	}
	req.Header.Set("Content-Type", contentType)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	req.Header.Set("Accept", "application/json")
	response, e := client.Do(req)
	if e != nil {
		return nil, ErrDelivery
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 {
		return nil, ErrDelivery
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, 32769))
	if e != nil || len(body) > 32768 || !utf8.Valid(body) {
		return nil, ErrDelivery
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if checkUniqueJSON(decoder, 0) != nil {
		return nil, ErrDelivery
	}
	if _, e = decoder.Token(); e != io.EOF {
		return nil, ErrDelivery
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, ErrDelivery
	}
	return fields, nil
}

func checkUniqueJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrDelivery
	}
	token, e := d.Token()
	if e != nil {
		return e
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrDelivery
			}
			seen[name] = true
			if e = checkUniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return ErrDelivery
		}
	case '[':
		for d.More() {
			if e = checkUniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return ErrDelivery
		}
	default:
		return ErrDelivery
	}
	return nil
}
