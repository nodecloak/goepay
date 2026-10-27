// Package compat implements the evidenced PHP payment signature formats.
// Strict form parsing intentionally rejects ambiguous parameter encodings rather
// than guessing PHP's version-dependent scalar/array overwrite behavior.
package compat

import (
	"crypto"
	"crypto/md5" // Required by the legacy public wire protocol only.
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Params map[string]string
type Profile uint8

const (
	// ProfileProServer matches app/lib/Payment.php and app/common.php.
	ProfileProServer Profile = iota
	// ProfileSDKV2 matches SDK_2.0.zip EpayCore::isEmpty.
	ProfileSDKV2
)

var ErrSignature = errors.New("invalid signature")
var ErrTimestamp = errors.New("invalid or expired timestamp")

// Parse consumes already URL-decoded values. Arrays/files must be decoded in a
// separate typed endpoint and validated independently; this scalar boundary
// never silently drops unsigned array parameters.
func Parse(raw url.Values) (Params, error) {
	p := make(Params, len(raw))
	for k, vs := range raw {
		if k == "" || strings.ContainsAny(k, "[] .\x00\t\r\n") || len(vs) != 1 {
			return nil, fmt.Errorf("ambiguous scalar parameter %q", k)
		}
		p[k] = vs[0]
	}
	return p, nil
}
func Canonical(p Params) string    { return CanonicalWithProfile(p, ProfileProServer) }
func CanonicalSDK(p Params) string { return CanonicalWithProfile(p, ProfileSDKV2) }
func CanonicalWithProfile(p Params, profile Profile) string {
	keys := make([]string, 0, len(p))
	for k, v := range p {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		if profile == ProfileSDKV2 && strings.Trim(v, " \t\n\r\x00\x0b") == "" {
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
func SignMD5(p Params, key string) string { return SignMD5WithProfile(p, key, ProfileProServer) }
func SignMD5WithProfile(p Params, key string, profile Profile) string {
	hash := md5.Sum([]byte(CanonicalWithProfile(p, profile) + key))
	return hex.EncodeToString(hash[:])
}
func VerifyMD5(p Params, key, signature string) bool {
	return subtle.ConstantTimeCompare([]byte(SignMD5(p, key)), []byte(signature)) == 1
}
func SignRSA(p Params, key *rsa.PrivateKey) (string, error) {
	return SignRSAWithProfile(p, key, ProfileProServer)
}
func SignRSAWithProfile(p Params, key *rsa.PrivateKey, profile Profile) (string, error) {
	if key == nil || key.N.BitLen() < 2048 {
		return "", errors.New("RSA private key must be at least 2048 bits")
	}
	digest := sha256.Sum256([]byte(CanonicalWithProfile(p, profile)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}
func VerifyRSA(p Params, signature string, key *rsa.PublicKey) bool {
	return VerifyRSAWithProfile(p, signature, key, ProfileProServer)
}
func VerifyRSAWithProfile(p Params, signature string, key *rsa.PublicKey, profile Profile) bool {
	if key == nil || key.N.BitLen() < 2048 {
		return false
	}
	signatureBytes, err := base64.StdEncoding.Strict().DecodeString(signature)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(CanonicalWithProfile(p, profile)))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signatureBytes) == nil
}

// ValidateTimestamp uses integer seconds and an inclusive window. Subtracting
// timestamps as time.Duration would overflow for hostile input, so compare bounds.
func ValidateTimestamp(raw string, now time.Time, window time.Duration) error {
	if raw == "" || window <= 0 {
		return ErrTimestamp
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return ErrTimestamp
		}
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return ErrTimestamp
	}
	margin := int64(window / time.Second)
	if seconds < now.Unix()-margin || seconds > now.Unix()+margin {
		return ErrTimestamp
	}
	return nil
}

type Credentials struct {
	Algorithm string
	// Nil keeps the existing strict algorithm. Explicit 0 accepts both original
	// algorithms; explicit 1 accepts RSA only. This comes from merchant storage.
	Keytype         *int
	MD5Key          string
	PublicKey       *rsa.PublicKey
	Profile         Profile
	TimestampWindow time.Duration
}

func (c Credentials) Allows(algorithm string) bool {
	if algorithm != "MD5" && algorithm != "RSA" {
		return false
	}
	if c.Keytype == nil {
		return algorithm == c.Algorithm
	}
	switch *c.Keytype {
	case 0:
		return true
	case 1:
		return algorithm == "RSA"
	default:
		return false
	}
}

// Verify uses merchant-owned algorithm configuration and the explicitly stored
// optional policy. sign_type only selects among permitted algorithms, never the
// policy. RSA requests in every mode require a fresh timestamp.
func Verify(p Params, c Credentials, now time.Time) error {
	if c.Profile != ProfileProServer && c.Profile != ProfileSDKV2 {
		return errors.New("unsupported signature profile")
	}
	algorithm := p["sign_type"]
	if algorithm == "" {
		algorithm = "MD5"
	}
	if !c.Allows(algorithm) {
		return ErrSignature
	}
	switch algorithm {
	case "MD5":
		if c.MD5Key == "" {
			return ErrSignature
		}
		expected := SignMD5WithProfile(p, c.MD5Key, c.Profile)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(p["sign"])) != 1 {
			return ErrSignature
		}
	case "RSA":
		if !VerifyRSAWithProfile(p, p["sign"], c.PublicKey, c.Profile) {
			return ErrSignature
		}
		window := c.TimestampWindow
		if window == 0 {
			window = 300 * time.Second
		}
		if err := ValidateTimestamp(p["timestamp"], now, window); err != nil {
			return err
		}
	default:
		return ErrSignature
	}
	return nil
}
func keyDER(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if block, rest := pem.Decode([]byte(raw)); block != nil {
		if strings.TrimSpace(string(rest)) != "" {
			return nil, errors.New("unexpected data after PEM key")
		}
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
}
func ParsePublicKey(raw string) (*rsa.PublicKey, error) {
	der, err := keyDER(raw)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	var key *rsa.PublicKey
	if err == nil {
		key, _ = parsed.(*rsa.PublicKey)
	} else {
		key, err = x509.ParsePKCS1PublicKey(der)
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA public key (minimum 2048 bits)")
	}
	return key, nil
}
func ParsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	der, err := keyDER(raw)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	var key *rsa.PrivateKey
	if err == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else {
		key, err = x509.ParsePKCS1PrivateKey(der)
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA private key (minimum 2048 bits)")
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return key, nil
}
