// Package payment contains provider wire adapters, isolated from ledger state.
package payment

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"goepay/internal/compat"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var ErrAuthentication = errors.New("provider authentication failed")

func canonical(p map[string]string, excludeType bool) string {
	keys := make([]string, 0, len(p))
	for k, v := range p {
		if k == "sign" || v == "" || excludeType && k == "sign_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+p[k])
	}
	return strings.Join(parts, "&")
}
func WechatV2Canonical(p map[string]string) string { return canonical(p, false) }
func SignWechatV2(p map[string]string, key, algorithm string) (string, error) {
	if key == "" {
		return "", ErrAuthentication
	}
	message := WechatV2Canonical(p) + "&key=" + key
	var digest []byte
	switch algorithm {
	case "MD5":
		h := md5.Sum([]byte(message))
		digest = h[:]
	case "HMAC-SHA256":
		h := hmac.New(sha256.New, []byte(key))
		h.Write([]byte(message))
		digest = h.Sum(nil)
	default:
		return "", errors.New("unsupported WeChat APIv2 signature algorithm")
	}
	return strings.ToUpper(hex.EncodeToString(digest)), nil
}
func VerifyWechatV2(p map[string]string, key, algorithm, signature string) bool {
	want, e := SignWechatV2(p, key, algorithm)
	return e == nil && subtle.ConstantTimeCompare([]byte(want), []byte(signature)) == 1
}
func signRSA(message []byte, key *rsa.PrivateKey) (string, error) {
	if key == nil || key.N.BitLen() < 2048 {
		return "", ErrAuthentication
	}
	d := sha256.Sum256(message)
	b, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, d[:])
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
func verifyRSA(message []byte, signature string, key *rsa.PublicKey) bool {
	if key == nil || key.N.BitLen() < 2048 {
		return false
	}
	b, e := base64.StdEncoding.Strict().DecodeString(signature)
	if e != nil {
		return false
	}
	d := sha256.Sum256(message)
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, d[:], b) == nil
}
func WechatV3RequestMessage(method, target, timestamp, nonce string, body []byte) ([]byte, error) {
	u, e := url.ParseRequestURI(target)
	if e != nil || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || u.IsAbs() || u.Fragment != "" || strings.ContainsAny(method+timestamp+nonce, "\r\n") || method == "" || nonce == "" {
		return nil, errors.New("invalid WeChat signing fields")
	}
	for _, c := range timestamp {
		if c < '0' || c > '9' {
			return nil, ErrAuthentication
		}
	}
	if timestamp == "" {
		return nil, ErrAuthentication
	}
	return []byte(method + "\n" + target + "\n" + timestamp + "\n" + nonce + "\n" + string(body) + "\n"), nil
}
func SignWechatV3Request(method, target, timestamp, nonce string, body []byte, key *rsa.PrivateKey) (string, error) {
	b, e := WechatV3RequestMessage(method, target, timestamp, nonce, body)
	if e != nil {
		return "", e
	}
	return signRSA(b, key)
}
func VerifyWechatV3Message(headers http.Header, body []byte, trusted map[string]*rsa.PublicKey, now time.Time, window time.Duration) error {
	values := map[string]string{}
	for _, name := range []string{"Wechatpay-Timestamp", "Wechatpay-Nonce", "Wechatpay-Serial", "Wechatpay-Signature"} {
		vs := headers.Values(name)
		if len(vs) != 1 || vs[0] == "" || strings.ContainsAny(vs[0], "\r\n") {
			return ErrAuthentication
		}
		values[name] = vs[0]
	}
	if e := compat.ValidateTimestamp(values["Wechatpay-Timestamp"], now, window); e != nil {
		return ErrAuthentication
	}
	message := values["Wechatpay-Timestamp"] + "\n" + values["Wechatpay-Nonce"] + "\n" + string(body) + "\n"
	if !verifyRSA([]byte(message), values["Wechatpay-Signature"], trusted[values["Wechatpay-Serial"]]) {
		return ErrAuthentication
	}
	return nil
}

type EncryptedResource struct {
	Algorithm      string `json:"algorithm"`
	Nonce          string `json:"nonce"`
	AssociatedData string `json:"associated_data"`
	Ciphertext     string `json:"ciphertext"`
}

// DecryptWechatResource does not authenticate the enclosing callback. Call
// VerifyWechatV3Message on original bytes first, then decode/decrypt the resource.
func DecryptWechatResource(key []byte, r EncryptedResource) ([]byte, error) {
	if len(key) != 32 || len(r.Nonce) != 12 || r.Algorithm != "AEAD_AES_256_GCM" {
		return nil, ErrAuthentication
	}
	b, e := base64.StdEncoding.Strict().DecodeString(r.Ciphertext)
	if e != nil || len(b) < 16 {
		return nil, ErrAuthentication
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	out, e := gcm.Open(nil, []byte(r.Nonce), b, []byte(r.AssociatedData))
	if e != nil {
		return nil, ErrAuthentication
	}
	return out, nil
}
func AlipayRequestCanonical(p map[string]string) string      { return alipayCanonical(p, false) }
func AlipayNotificationCanonical(p map[string]string) string { return alipayCanonical(p, true) }
func alipayCanonical(p map[string]string, notification bool) string {
	filtered := map[string]string{}
	for k, v := range p {
		if strings.Trim(v, " \t\n\r\x00\x0b") == "" || strings.HasPrefix(v, "@") {
			continue
		}
		filtered[k] = v
	}
	return canonical(filtered, notification)
}
func SignAlipayRSA2(p map[string]string, key *rsa.PrivateKey) (string, error) {
	if p["sign_type"] != "RSA2" {
		return "", ErrAuthentication
	}
	return signRSA([]byte(AlipayRequestCanonical(p)), key)
}
func VerifyAlipayNotification(p map[string]string, key *rsa.PublicKey) bool {
	return p["sign_type"] == "RSA2" && verifyRSA([]byte(AlipayNotificationCanonical(p)), p["sign"], key)
}
