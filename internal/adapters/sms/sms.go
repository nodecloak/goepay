// Package sms ports epay_release/includes/lib/sms/{Aliyun,Qcloud,SmsBao}.php.
// Each adapter makes exactly one upstream HTTP call to send one verification
// code to one phone, returning a typed outcome. The base URL is injectable so
// httptest can exercise every request without reaching a real provider; the
// default is the original endpoint. No credential is logged.
package sms

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// md5hex is required by the SmsBao upstream protocol (it hashes the account
// password with MD5); this is the provider's wire contract, not our own
// credential storage, which is AES-GCM sealed in smsgateway134.
func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", sum)
}

// ErrConfig is a syntactic configuration problem (empty keys). ErrRejected is a
// definite upstream rejection. ErrUnknown is an unconfirmed outcome (network,
// non-2xx, unparseable) — the caller must NOT treat it as sent.
var (
	ErrConfig   = errors.New("sms provider not configured")
	ErrRejected = errors.New("sms provider rejected the message")
	ErrUnknown  = errors.New("sms send outcome unconfirmed")
)

// Sender sends one code to one phone under the given sign/template.
type Sender interface {
	Send(ctx context.Context, phone, code string) error
}

func newClient() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

// --- Aliyun (dysmsapi.aliyuncs.com, SendSms, HMAC-SHA1) --------------------

type Aliyun struct {
	AccessKeyID     string
	AccessKeySecret string
	SignName        string
	TemplateCode    string
	BaseURL         string // default https://dysmsapi.aliyuncs.com/
	Client          *http.Client
	Now             func() time.Time
	Nonce           func() string
}

func (a Aliyun) Send(ctx context.Context, phone, code string) error {
	if a.AccessKeyID == "" || a.AccessKeySecret == "" || a.SignName == "" || a.TemplateCode == "" {
		return ErrConfig
	}
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	nonce := func() string { return strconv.FormatInt(now().UnixNano(), 10) }
	if a.Nonce != nil {
		nonce = a.Nonce
	}
	params := map[string]string{
		"Action":           "SendSms",
		"PhoneNumbers":     phone,
		"SignName":         a.SignName,
		"TemplateCode":     a.TemplateCode,
		"TemplateParam":    `{"code":"` + code + `"}`,
		"Format":           "JSON",
		"RegionId":         "cn-hangzhou",
		"Version":          "2017-05-25",
		"AccessKeyId":      a.AccessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        now().UTC().Format("2006-01-02T15:04:05Z"),
		"SignatureVersion": "1.0",
		"SignatureNonce":   nonce(),
	}
	params["Signature"] = aliyunSignature(params, a.AccessKeySecret)
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	base := a.BaseURL
	if base == "" {
		base = "https://dysmsapi.aliyuncs.com/"
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, base, strings.NewReader(form.Encode()))
	if e != nil {
		return ErrUnknown
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := a.Client
	if client == nil {
		client = newClient()
	}
	resp, e := client.Do(req)
	if e != nil {
		return ErrUnknown
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if e != nil || resp.StatusCode != 200 {
		return ErrUnknown
	}
	var out struct {
		Code string `json:"Code"`
	}
	if json.Unmarshal(body, &out) != nil || out.Code == "" {
		return ErrUnknown
	}
	if out.Code == "OK" {
		return nil
	}
	return ErrRejected
}

func aliyunPercent(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}
func aliyunSignature(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "Signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(aliyunPercent(k) + "=" + aliyunPercent(params[k]))
	}
	str := "POST&%2F&" + aliyunPercent(b.String())
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(str))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// --- Qcloud (yun.tim.qq.com/v5/tlssmssvr/sendsms, SHA256 sig) --------------

type Qcloud struct {
	AppID   string
	AppKey  string
	TplID   string
	Sign    string
	BaseURL string // default https://yun.tim.qq.com/v5/tlssmssvr/sendsms
	Client  *http.Client
	Now     func() time.Time
	Random  func() int
}

func (q Qcloud) Send(ctx context.Context, phone, code string) error {
	if q.AppID == "" || q.AppKey == "" || q.TplID == "" || q.Sign == "" {
		return ErrConfig
	}
	now := time.Now
	if q.Now != nil {
		now = q.Now
	}
	rnd := func() int { return int(now().UnixNano() % 900000) }
	if q.Random != nil {
		rnd = q.Random
	}
	ts := now().Unix()
	random := rnd()
	tpl, e := strconv.Atoi(q.TplID)
	if e != nil {
		return ErrConfig
	}
	sig := sha256.Sum256([]byte(fmt.Sprintf("appkey=%s&random=%d&time=%d&mobile=%s", q.AppKey, random, ts, phone)))
	payload := map[string]any{
		"tel":    map[string]string{"nationcode": "86", "mobile": phone},
		"params": []string{code},
		"time":   ts,
		"tpl_id": tpl,
		"sign":   q.Sign,
		"sig":    fmt.Sprintf("%x", sig),
	}
	raw, e := json.Marshal(payload)
	if e != nil {
		return ErrUnknown
	}
	base := q.BaseURL
	if base == "" {
		base = "https://yun.tim.qq.com/v5/tlssmssvr/sendsms"
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s?sdkappid=%s&random=%d", base, q.AppID, random), strings.NewReader(string(raw)))
	if e != nil {
		return ErrUnknown
	}
	req.Header.Set("Content-Type", "application/json; charset=utf8")
	client := q.Client
	if client == nil {
		client = newClient()
	}
	resp, e := client.Do(req)
	if e != nil {
		return ErrUnknown
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if e != nil || resp.StatusCode != 200 {
		return ErrUnknown
	}
	var out struct {
		Result int `json:"result"`
	}
	if json.Unmarshal(body, &out) != nil {
		return ErrUnknown
	}
	if out.Result == 0 {
		return nil
	}
	return ErrRejected
}

// --- SmsBao (api.smsbao.com/sms, md5 pass, plain-text "0" ok) --------------

type SmsBao struct {
	User     string
	Pass     string // the password; the adapter md5s it like the original
	Template string // "{code}" placeholder
	Sign     string
	BaseURL  string // default http://api.smsbao.com
	Client   *http.Client
}

func (s SmsBao) Send(ctx context.Context, phone, code string) error {
	if s.User == "" || s.Pass == "" || s.Template == "" || s.Sign == "" {
		return ErrConfig
	}
	content := "【" + s.Sign + "】" + strings.ReplaceAll(s.Template, "{code}", code)
	base := s.BaseURL
	if base == "" {
		base = "http://api.smsbao.com"
	}
	v := url.Values{}
	v.Set("u", s.User)
	v.Set("p", md5hex(s.Pass))
	v.Set("m", phone)
	v.Set("c", content)
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"/sms?"+v.Encode(), nil)
	if e != nil {
		return ErrUnknown
	}
	client := s.Client
	if client == nil {
		client = newClient()
	}
	resp, e := client.Do(req)
	if e != nil {
		return ErrUnknown
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if e != nil || resp.StatusCode != 200 {
		return ErrUnknown
	}
	switch strings.TrimSpace(string(body)) {
	case "0":
		return nil
	case "":
		return ErrUnknown
	default:
		return ErrRejected
	}
}
