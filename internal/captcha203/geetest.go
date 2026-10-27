package captcha203

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type geetestVerifier struct{ client *http.Client }

func NewGeetestVerifier(client *http.Client) Verifier {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copyClient := *client
	if copyClient.Timeout <= 0 || copyClient.Timeout > 5*time.Second {
		copyClient.Timeout = 5 * time.Second
	}
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &geetestVerifier{client: &copyClient}
}

var identifier = regexp.MustCompile(`^[a-f0-9]{32}$`)

func validConfig(c Config) bool {
	return (c.Version == 0 || c.Version == 1) && identifier.MatchString(c.ID) && len(c.Key) >= 16 && len(c.Key) <= 256 && !strings.ContainsAny(c.Key, "\r\n\x00")
}
func digestMD5(s string) string { d := md5.Sum([]byte(s)); return hex.EncodeToString(d[:]) }
func equal(a, b string) bool    { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (g *geetestVerifier) request(ctx context.Context, method, target string, values url.Values) (map[string]json.RawMessage, error) {
	var body io.Reader
	if method == http.MethodGet && len(values) > 0 {
		target += "?" + values.Encode()
	} else if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, target, body)
	if e != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, e := g.client.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 16385))
	if e != nil || len(raw) > 16384 {
		return nil, ErrUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrUnavailable
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, e := d.Token()
		k, ok := token.(string)
		if e != nil || !ok {
			return nil, ErrUnavailable
		}
		if _, ok = out[k]; ok {
			return nil, ErrUnavailable
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, ErrUnavailable
		}
		out[k] = value
	}
	if _, e = d.Token(); e != nil {
		return nil, ErrUnavailable
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return nil, ErrUnavailable
	}
	return out, nil
}
func stringField(m map[string]json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(m[key], &s) != nil {
		return ""
	}
	return s
}
func (g *geetestVerifier) Init(ctx context.Context, c Config, purpose, source string) (Challenge, error) {
	if !validConfig(c) || purpose != "testpay" && purpose != "login" && purpose != "payment-defend" {
		return Challenge{}, ErrUnavailable
	}
	if c.Version == 1 {
		return Challenge{Version: 1, GT: c.ID, Success: 1}, nil
	}
	out, e := g.request(ctx, "GET", "https://api.geetest.com/register.php", url.Values{"gt": {c.ID}, "digestmod": {"md5"}, "sdk": {"php_3.0.0"}, "json_format": {"1"}, "client_type": {"web"}, "ip_address": {source}})
	if e != nil {
		return Challenge{}, e
	}
	challenge := stringField(out, "challenge")
	if !identifier.MatchString(challenge) {
		return Challenge{}, ErrUnavailable
	}
	return Challenge{Version: 0, GT: c.ID, Challenge: digestMD5(challenge + c.Key), NewCaptcha: true, Success: 1}, nil
}
func (g *geetestVerifier) Verify(ctx context.Context, c Config, issued Challenge, p Proof, source string) error {
	if !validConfig(c) || issued.Version != c.Version || issued.GT != c.ID || issued.Success != 1 {
		return ErrInvalid
	}
	if c.Version == 0 {
		if !identifier.MatchString(issued.Challenge) || !equal(issued.Challenge, p.GeetestChallenge) || !identifier.MatchString(p.GeetestValidate) || !equal(digestMD5(c.Key+"geetest"+p.GeetestChallenge), p.GeetestValidate) || len(p.GeetestSeccode) < 1 || len(p.GeetestSeccode) > 1024 || p.LotNumber != "" || p.CaptchaID != "" || p.PassToken != "" || p.GenTime != "" || p.CaptchaOutput != "" {
			return ErrInvalid
		}
		out, e := g.request(ctx, "POST", "https://api.geetest.com/validate.php", url.Values{"challenge": {p.GeetestChallenge}, "seccode": {p.GeetestSeccode}, "captchaid": {c.ID}, "sdk": {"php_3.0.0"}, "json_format": {"1"}, "ip_address": {source}})
		if e != nil {
			return e
		}
		if !equal(stringField(out, "seccode"), digestMD5(p.GeetestSeccode)) {
			return ErrInvalid
		}
		return nil
	}
	stamp, e := strconv.ParseInt(p.GenTime, 10, 64)
	if e != nil || stamp < time.Now().Add(-10*time.Minute).Unix() || stamp > time.Now().Add(time.Minute).Unix() || p.CaptchaID != c.ID || !identifier.MatchString(p.LotNumber) || len(p.PassToken) < 1 || len(p.PassToken) > 2048 || len(p.CaptchaOutput) < 1 || len(p.CaptchaOutput) > 4096 || p.GeetestChallenge != "" || p.GeetestValidate != "" || p.GeetestSeccode != "" {
		return ErrInvalid
	}
	mac := hmac.New(sha256.New, []byte(c.Key))
	mac.Write([]byte(p.LotNumber))
	values := url.Values{"lot_number": {p.LotNumber}, "pass_token": {p.PassToken}, "gen_time": {p.GenTime}, "captcha_output": {p.CaptchaOutput}, "sign_token": {hex.EncodeToString(mac.Sum(nil))}}
	out, e := g.request(ctx, "POST", "https://gcaptcha4.geetest.com/validate?captcha_id="+url.QueryEscape(c.ID), values)
	if e != nil {
		return e
	}
	if stringField(out, "status") != "success" || stringField(out, "result") != "success" {
		return ErrInvalid
	}
	if raw, ok := out["captcha_args"]; ok {
		var args map[string]json.RawMessage
		if json.Unmarshal(raw, &args) != nil {
			return ErrInvalid
		}
		for field, want := range map[string]string{"captcha_id": c.ID, "lot_number": p.LotNumber} {
			if value, exists := args[field]; exists {
				var actual string
				if json.Unmarshal(value, &actual) != nil || !equal(actual, want) {
					return ErrInvalid
				}
			}
		}
	}
	return nil
}
