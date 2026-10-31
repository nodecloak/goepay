// Package goepay is a standard-library V2 merchant client for the Go payment
// service. It does not initiate requests until an API method is explicitly called.
package goepay

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrSignature = errors.New("goepay: signature verification failed")
	ErrTimestamp = errors.New("goepay: timestamp outside the inclusive 300-second window")
	ErrProtocol  = errors.New("goepay: unexpected or unsupported response")
)

func scalarValues(values url.Values) (map[string]string, error) {
	fields := make(map[string]string, len(values))
	for key, values := range values {
		if key == "" || strings.ContainsAny(key, "[] .\x00\t\r\n") || !utf8.ValidString(key) || len(values) != 1 || !utf8.ValidString(values[0]) {
			return nil, fmt.Errorf("goepay: ambiguous scalar parameter %q", key)
		}
		fields[key] = values[0]
	}
	return fields, nil
}

// Requests preserve whitespace-only values. Responses and V2 notifications
// omit values containing only the protocol's ASCII whitespace characters.
func canonical(fields map[string]string, reply bool) string {
	keys := make([]string, 0, len(fields))
	for key, value := range fields {
		if key == "sign" || key == "sign_type" || value == "" || (reply && strings.Trim(value, " \t\n\r\x00\x0b") == "") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fields[key])
	}
	return strings.Join(parts, "&")
}

func sign(fields map[string]string, key *rsa.PrivateKey) (string, error) {
	digest := sha256.Sum256([]byte(canonical(fields, false)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func verify(fields map[string]string, key *rsa.PublicKey, now time.Time) error {
	if fields["sign_type"] != "RSA" || fields["sign"] == "" {
		return ErrSignature
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(fields["sign"])
	if err != nil {
		return ErrSignature
	}
	digest := sha256.Sum256([]byte(canonical(fields, true)))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return ErrSignature
	}
	stamp := fields["timestamp"]
	if stamp == "" {
		return ErrTimestamp
	}
	for _, c := range stamp {
		if c < '0' || c > '9' {
			return ErrTimestamp
		}
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || seconds <= 0 || seconds < now.Unix()-300 || seconds > now.Unix()+300 {
		return ErrTimestamp
	}
	return nil
}

func pemKey(raw []byte) (*pem.Block, error) {
	raw = bytes.TrimSpace(raw)
	if !bytes.HasPrefix(raw, []byte("-----BEGIN ")) {
		return nil, errors.New("goepay: expected one unencrypted PEM key")
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(block.Headers) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("goepay: expected one unencrypted PEM key")
	}
	return block, nil
}

func privateKey(raw []byte) (*rsa.PrivateKey, error) {
	block, err := pemKey(raw)
	if err != nil {
		return nil, err
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*rsa.PrivateKey)
	default:
		err = errors.New("unsupported private PEM type")
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, errors.New("goepay: invalid RSA private key; minimum 2048 bits")
	}
	return key, nil
}

func publicKey(raw []byte) (*rsa.PublicKey, error) {
	block, err := pemKey(raw)
	if err != nil {
		return nil, err
	}
	var key *rsa.PublicKey
	switch block.Type {
	case "RSA PUBLIC KEY":
		key, err = x509.ParsePKCS1PublicKey(block.Bytes)
	case "PUBLIC KEY":
		var parsed any
		parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
		key, _ = parsed.(*rsa.PublicKey)
	default:
		err = errors.New("unsupported public PEM type")
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 {
		return nil, errors.New("goepay: invalid RSA public key; minimum 2048 bits")
	}
	return key, nil
}
