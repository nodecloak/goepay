package payment

import (
	"bytes"
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"strings"
	"time"
)

// CertificateMode is explicit, entirely offline trust material. PEM values are
// certificate contents, never filesystem paths or download URLs.
type AlipayCertificateConfig struct {
	ApplicationPEM, PlatformPEM, RootPEM string
}

type alipayCertificateState struct {
	application   *x509.Certificate
	platform      map[string]*x509.Certificate
	roots         *x509.CertPool
	appSN, rootSN string
}

func parseAlipayCertificates(raw string) ([]*x509.Certificate, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	var result []*x509.Certificate
	rest := []byte(raw)
	for len(bytes.TrimSpace(rest)) > 0 {
		if !bytes.HasPrefix(bytes.TrimSpace(rest), []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, ErrInvalid
		}
		block, next := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) > 0 {
			return nil, ErrInvalid
		}
		cert, e := x509.ParseCertificate(block.Bytes)
		if e != nil || cert.SerialNumber.Sign() <= 0 {
			return nil, ErrInvalid
		}
		result = append(result, cert)
		rest = next
	}
	if len(result) == 0 {
		return nil, ErrInvalid
	}
	return result, nil
}

// The supplied PHP helper reverses OpenSSL issuer attributes without escaping.
// Repeated or unknown attributes are rejected rather than guessing an SN.
func alipayIssuer(c *x509.Certificate) (string, error) {
	var rdns pkix.RDNSequence
	if rest, e := asn1.Unmarshal(c.RawIssuer, &rdns); e != nil || len(rest) > 0 {
		return "", ErrInvalid
	}
	labels := map[string]string{"2.5.4.3": "CN", "2.5.4.6": "C", "2.5.4.7": "L", "2.5.4.8": "ST", "2.5.4.10": "O", "2.5.4.11": "OU", "2.5.4.5": "serialNumber", "2.5.4.4": "SN", "2.5.4.42": "GN", "2.5.4.12": "title", "2.5.4.43": "initials", "0.9.2342.19200300.100.1.25": "DC", "0.9.2342.19200300.100.1.1": "UID", "1.2.840.113549.1.9.1": "emailAddress"}
	seen := map[string]bool{}
	var parts []string
	for _, rdn := range rdns {
		for _, attribute := range rdn {
			label := labels[attribute.Type.String()]
			value, ok := attribute.Value.(string)
			if label == "" || !ok || seen[label] {
				return "", ErrInvalid
			}
			seen[label] = true
			parts = append(parts, label+"="+value)
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ","), nil
}

func alipayCertSN(c *x509.Certificate) (string, error) {
	issuer, e := alipayIssuer(c)
	if e != nil {
		return "", e
	}
	serial := c.SerialNumber.String()
	// Decimal big integers match the supplied helper on PHP 8.2/OpenSSL and
	// the official SDK rule; no conversion through int64 or floating point.
	digest := md5.Sum([]byte(issuer + serial))
	return hex.EncodeToString(digest[:]), nil
}

func newAlipayCertificateState(cfg *AlipayCertificateConfig, private *rsa.PrivateKey, public *rsa.PublicKey, now time.Time) (*alipayCertificateState, error) {
	apps, e := parseAlipayCertificates(cfg.ApplicationPEM)
	if e != nil || len(apps) != 1 {
		return nil, ErrInvalid
	}
	platform, e := parseAlipayCertificates(cfg.PlatformPEM)
	if e != nil {
		return nil, ErrInvalid
	}
	roots, e := parseAlipayCertificates(cfg.RootPEM)
	if e != nil {
		return nil, ErrInvalid
	}
	state := &alipayCertificateState{application: apps[0], platform: map[string]*x509.Certificate{}, roots: x509.NewCertPool()}
	var rootSN []string
	seen := map[string]bool{}
	for _, cert := range roots {
		if !cert.IsCA || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			return nil, ErrInvalid
		}
		state.roots.AddCert(cert)
		if cert.SignatureAlgorithm == x509.SHA1WithRSA || cert.SignatureAlgorithm == x509.SHA256WithRSA {
			sn, e := alipayCertSN(cert)
			if e != nil || seen[sn] {
				return nil, ErrInvalid
			}
			rootSN = append(rootSN, sn)
			seen[sn] = true
		}
	}
	if len(rootSN) == 0 {
		return nil, ErrInvalid
	}
	state.rootSN = strings.Join(rootSN, "_")
	appKey, ok := apps[0].PublicKey.(*rsa.PublicKey)
	if !ok || appKey.N.BitLen() < 2048 || appKey.N.Cmp(private.N) != 0 || appKey.E != private.E || !state.trusted(apps[0], now) {
		return nil, ErrInvalid
	}
	state.appSN, e = alipayCertSN(apps[0])
	if e != nil {
		return nil, e
	}
	for _, cert := range platform {
		key, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok || key.N.BitLen() < 2048 || !state.trusted(cert, now) {
			return nil, ErrInvalid
		}
		sn, e := alipayCertSN(cert)
		if e != nil {
			return nil, e
		}
		if previous := state.platform[sn]; previous != nil && !bytes.Equal(previous.Raw, cert.Raw) {
			return nil, ErrInvalid
		}
		state.platform[sn] = cert
	}
	if public != nil {
		matched := false
		for _, cert := range platform {
			key := cert.PublicKey.(*rsa.PublicKey)
			if key.N.Cmp(public.N) == 0 && key.E == public.E {
				matched = true
			}
		}
		if !matched {
			return nil, ErrInvalid
		}
	}
	return state, nil
}

func (s *alipayCertificateState) trusted(cert *x509.Certificate, now time.Time) bool {
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || cert.IsCA || cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return false
	}
	_, e := cert.Verify(x509.VerifyOptions{Roots: s.roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return e == nil
}
func (a *Alipay) certificateKey(sn string) *rsa.PublicKey {
	cert := a.certificates.platform[sn]
	if cert == nil || !a.certificates.trusted(cert, a.config.Now()) {
		return nil
	}
	return cert.PublicKey.(*rsa.PublicKey)
}

func (a *Alipay) validateCertificates() error {
	if a.certificates == nil {
		return nil
	}
	if !a.certificates.trusted(a.certificates.application, a.config.Now()) {
		return ErrAuthentication
	}
	for sn := range a.certificates.platform {
		if a.certificateKey(sn) != nil {
			return nil
		}
	}
	return ErrAuthentication
}
func (a *Alipay) verifyResponse(node []byte, signature string, envelope map[string]json.RawMessage) bool {
	if a.certificates == nil {
		return verifyRSA(node, signature, a.config.PublicKey)
	}
	var sn string
	if json.Unmarshal(envelope["alipay_cert_sn"], &sn) != nil {
		return false
	}
	key := a.certificateKey(sn)
	return key != nil && verifyRSA(node, signature, key)
}
func (a *Alipay) verifyNotification(p map[string]string) bool {
	if a.certificates == nil {
		return VerifyAlipayNotification(p, a.config.PublicKey)
	}
	if sn := p["alipay_cert_sn"]; sn != "" {
		key := a.certificateKey(sn)
		return key != nil && VerifyAlipayNotification(p, key)
	}
	// Legacy asynchronous notifications need not carry alipay_cert_sn. Only
	// explicitly pinned, currently valid platform certificates may verify one.
	for sn := range a.certificates.platform {
		if key := a.certificateKey(sn); key != nil && VerifyAlipayNotification(p, key) {
			return true
		}
	}
	return false
}
