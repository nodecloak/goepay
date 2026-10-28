package payment

// Port of epay_release/plugins/jdpay (京东支付). The protocol is a 3DES-ECB +
// RSA XML scheme:
//   - Each business field is 3DES-ede3 (ECB, no padding) encrypted with a
//     custom 4-byte big-endian length prefix + zero pad, then hex-encoded.
//     The DES key is base64_decode(<商户DES密钥>).
//   - The create request posts a <form> to wepay.jd.com (PC) / h5pay.jd.com
//     (mobile) /jdpay/saveOrder, with "sign" = base64(RSA_private_encrypt(
//     sha256hex(ksorted k=v& of the PLAINTEXT params))) using the merchant
//     private key. The field values are then individually 3DES-hex encrypted.
//   - Refund builds <jdpay>…<sign>…</sign></jdpay>, 3DES-encrypts the whole
//     document, base64s it into an <encrypt>, and posts to paygate.jd.com.
//   - The async notify and refund reply are 3DES-encrypted XML whose inner
//     <sign> is verified by RSA_public_decrypt with JD's fixed platform public
//     key (bundled below) == sha256hex(xml-without-sign).
// The merchant RSA private key is a PEM input. Transfers are OUT OF SCOPE.

import (
	"context"
	"crypto/des"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// jdpayPlatformPublicKeyPEM is JD's fixed platform public key, shipped with
// the original plugin as inc/cert/wy_rsa_public_key.pem. It verifies JD's
// signed replies/notifications. It is public, not a secret.
const jdpayPlatformPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCKE5N2xm3NIrXON8Zj19GNtLZ8
xwEQ6uDIyrS3S03UhgBJMkGl4msfq4Xuxv6XUAN7oU1XhV3/xtabr9rXto4Ke3d6
WwNbxwXnK5LSgsQc1BhT5NcXHXpGBdt7P8NMez5qGieOKqHGvT0qvjyYnYA29a8Z
4wzNR7vAVHp36uD5RwIDAQAB
-----END PUBLIC KEY-----`

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "jdpay",
		Title:  "京东支付",
		Author: "京东支付",
		Link:   "https://www.jdpay.com/",
		Types:  []string{"jdpay"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "商户号", Required: true},
			{Key: "appkey", Label: "商户DES密钥", Secret: true, Required: true},
			{Key: "rsa_private_key", Label: "商户RSA私钥(seller_rsa_private_key.pem)", PEM: true, Textarea: true, Required: true},
		},
		Note:  "需要商户私钥seller_rsa_private_key.pem。",
		Build: newJdpayPlugin,
	})
}

const (
	jdpayPCSaveOrderURL     = "https://wepay.jd.com/jdpay/saveOrder"
	jdpayMobileSaveOrderURL = "https://h5pay.jd.com/jdpay/saveOrder"
	jdpayRefundURL          = "https://paygate.jd.com/service/refund"
)

type jdpayPlugin struct {
	pluginBase
	merchant    string
	desKey      []byte          // 24-byte 3DES key
	privateKey  *rsa.PrivateKey // merchant signing key
	platformPub *rsa.PublicKey  // JD fixed platform key
}

func newJdpayPlugin(c PluginChannel) (Provider, error) {
	merchant := strings.TrimSpace(c.Config["appid"])
	if merchant == "" || len(merchant) > 32 || strings.ContainsAny(merchant, "<>&\x00") {
		return nil, ErrInvalid
	}
	rawKey, e := base64.StdEncoding.DecodeString(strings.TrimSpace(c.Config["appkey"]))
	if e != nil {
		return nil, ErrInvalid
	}
	desKey, e := jdpayNormalizeDESKey(rawKey)
	if e != nil {
		return nil, ErrInvalid
	}
	priv, e := pluginPrivateKey(c.Certificates["rsa_private_key"])
	if e != nil {
		return nil, ErrInvalid
	}
	pub, e := jdpayParsePlatformKey()
	if e != nil {
		return nil, ErrInvalid
	}
	base, e := newPluginBase(c, pluginAccount("https://wepay.jd.com", merchant))
	if e != nil {
		return nil, e
	}
	return &jdpayPlugin{pluginBase: base, merchant: merchant, desKey: desKey, privateKey: priv, platformPub: pub}, nil
}

func (p *jdpayPlugin) methods() []string {
	// Page/H5 jump form only (saveOrder); no QR product exists. The pay type
	// is jdpay, so "h5" is the GOEPAY routing name for the page form.
	return []string{"h5"}
}

func (p *jdpayPlugin) Capabilities() Capabilities {
	return Capabilities{Methods: p.methods(), Refund: true}
}

func (p *jdpayPlugin) ValidateCreate(r CreateRequest) error {
	return validatePluginCreate(r, p.methods())
}

func (p *jdpayPlugin) Create(_ context.Context, r CreateRequest) (CreateResult, error) {
	if e := p.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	// Plaintext params signed, then field-encrypted. tradeTime is server
	// wall-clock, as the original uses date('YmdHis').
	plain := map[string]string{
		"version":     "V2.0",
		"merchant":    p.merchant,
		"tradeNum":    r.TradeNo,
		"tradeName":   r.Subject,
		"tradeTime":   time.Now().Format("20060102150405"),
		"amount":      strconv.FormatInt(r.Amount, 10),
		"currency":    "CNY",
		"callbackUrl": r.ReturnURL,
		"notifyUrl":   r.NotifyURL,
		"ip":          r.ClientIP,
		"userId":      "",
		"orderType":   "1",
	}
	sign, e := p.sign(plain)
	if e != nil {
		return CreateResult{}, e
	}
	// Build the posted form: version/merchant/sign plaintext, the rest TDES.
	form := map[string]string{
		"version":  plain["version"],
		"merchant": plain["merchant"],
		"sign":     sign,
	}
	for _, k := range []string{"tradeNum", "tradeName", "tradeTime", "amount", "currency", "callbackUrl", "notifyUrl", "ip", "userId", "orderType"} {
		v := plain[k]
		if v == "" {
			continue
		}
		enc, e := p.encryptHex(v)
		if e != nil {
			return CreateResult{}, e
		}
		form[k] = enc
	}
	action := jdpayPCSaveOrderURL
	// The original picks the mobile endpoint by device; "h5" is our mobile
	// method name, so use the mobile saveOrder host.
	action = jdpayMobileSaveOrderURL
	return CreateResult{MaterialType: "html", Material: pluginForm(action, form, "正在跳转"), ProviderTradeNo: r.TradeNo}, nil
}

// sign = base64(RSA_private_encrypt(sha256hex(signString))), signString being
// the ksort-ed k=v& join of the non-empty plaintext params minus "sign".
func (p *jdpayPlugin) sign(params map[string]string) (string, error) {
	digest := sha256Hex(jdpaySignString(params))
	enc, e := jdpayPrivateEncrypt(p.privateKey, []byte(digest))
	if e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

func (p *jdpayPlugin) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected ExpectedPayment) (PaymentEvent, error) {
	data, ok := p.decryptResXML(raw)
	if !ok {
		return PaymentEvent{}, ErrAuthentication
	}
	// status 2 == paid (the original's $param["status"]==2).
	if data["status"] != "2" {
		return PaymentEvent{}, ErrInvalid
	}
	amount, e := parseFen(data["amount"])
	if e != nil || expected.Amount <= 0 || amount != expected.Amount || data["tradeNum"] != expected.TradeNo {
		return PaymentEvent{}, ErrAuthentication
	}
	return paidEvent(expected.TradeNo, data["tradeNum"], "", amount), nil
}

// The original notify() echoes "success" / "error".
func (p *jdpayPlugin) NotificationAck(err error) (int, string, string) {
	if err != nil {
		return http.StatusOK, "text/plain; charset=utf-8", "error"
	}
	return http.StatusOK, "text/plain; charset=utf-8", "success"
}

func (p *jdpayPlugin) Refund(ctx context.Context, r RefundRequest) (RefundResult, error) {
	if r.ProviderTradeNo == "" || r.RefundNo == "" || r.Amount <= 0 {
		return RefundResult{}, ErrInvalid
	}
	param := map[string]string{
		"version":   "V2.0",
		"merchant":  p.merchant,
		"tradeNum":  r.RefundNo,
		"oTradeNum": r.ProviderTradeNo,
		"amount":    strconv.FormatInt(r.Amount, 10),
		"currency":  "CNY",
	}
	reqXML, e := p.encryptReqXML(param)
	if e != nil {
		return RefundResult{}, e
	}
	raw, e := p.pluginCall(ctx, "jdpay.refund", http.MethodPost, jdpayRefundURL, reqXML, "application/xml;charset=utf-8", nil)
	if e != nil {
		return RefundResult{}, e
	}
	data, ok := p.decryptResXML(raw)
	if !ok {
		return RefundResult{}, &UnknownOutcome{Operation: "jdpay.refund", Cause: errors.New("refund reply verification failed")}
	}
	if data["status"] != "1" {
		return RefundResult{}, rejected("JDPAY_" + jdpayCode(data["status"]))
	}
	return RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: data["oTradeNum"], State: "SUCCESS", Amount: r.Amount}, nil
}

// encryptReqXML mirrors XMLUtil::encryptReqXml: build <jdpay> doc, append
// <sign>=base64(RSA_private_encrypt(sha256hex(docWithoutSign))), 3DES-hex the
// whole doc, base64 it into <encrypt>, wrap with version/merchant.
func (p *jdpayPlugin) encryptReqXML(param map[string]string) ([]byte, error) {
	keys := jdpayOrderedKeys(param)
	inner := jdpayBuildXML("jdpay", keys, param)
	digest := sha256Hex(inner)
	signRaw, e := jdpayPrivateEncrypt(p.privateKey, []byte(digest))
	if e != nil {
		return nil, e
	}
	sign := base64.StdEncoding.EncodeToString(signRaw)
	signed := inner[:len(inner)-len("</jdpay>")] + "<sign>" + jdpayXMLEscape(sign) + "</sign></jdpay>"
	encHex, e := p.encryptHex(signed)
	if e != nil {
		return nil, e
	}
	encrypt := base64.StdEncoding.EncodeToString([]byte(encHex))
	req := map[string]string{"version": param["version"], "merchant": param["merchant"], "encrypt": encrypt}
	return []byte(jdpayBuildXML("jdpay", []string{"version", "merchant", "encrypt"}, req)), nil
}

// decryptResXML mirrors XMLUtil::decryptResXml: read <encrypt>, base64-decode,
// 3DES-hex-decrypt, parse inner <jdpay>, verify <sign> == RSA_public_decrypt
// with the platform key == sha256hex(innerWithoutSign).
func (p *jdpayPlugin) decryptResXML(raw []byte) (map[string]string, bool) {
	outer, e := jdpayParseFlat(raw)
	if e != nil {
		return nil, false
	}
	encB64 := outer["encrypt"]
	if encB64 == "" {
		return nil, false
	}
	encHex, e := base64.StdEncoding.DecodeString(encB64)
	if e != nil {
		return nil, false
	}
	inner, e := p.decryptHex(string(encHex))
	if e != nil {
		return nil, false
	}
	data, e := jdpayParseFlat([]byte(inner))
	if e != nil {
		return nil, false
	}
	inputSign := data["sign"]
	if inputSign == "" {
		return nil, false
	}
	// The original strips the <sign>…</sign> substring and hashes the rest.
	start := strings.Index(inner, "<sign>")
	end := strings.Index(inner, "</sign>")
	if start < 0 || end < 0 {
		return nil, false
	}
	stripped := inner[:start] + inner[end+len("</sign>"):]
	want := sha256Hex(stripped)
	sigBytes, e := base64.StdEncoding.DecodeString(inputSign)
	if e != nil {
		return nil, false
	}
	got, e := jdpayPublicDecrypt(p.platformPub, sigBytes)
	if e != nil || string(got) != want {
		return nil, false
	}
	delete(data, "sign")
	return data, true
}

// ---- 3DES (ECB, no padding) with JD's length-prefix padding ----

func (p *jdpayPlugin) encryptHex(s string) (string, error) {
	padded := jdpayPad([]byte(s))
	out, e := jdpayDESECB(p.desKey, padded, true)
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(out), nil
}

func (p *jdpayPlugin) decryptHex(hexStr string) (string, error) {
	raw, e := hex.DecodeString(hexStr)
	if e != nil {
		return "", e
	}
	out, e := jdpayDESECB(p.desKey, raw, false)
	if e != nil {
		return "", e
	}
	return jdpayUnpad(out)
}

// jdpayPad: 4-byte big-endian length prefix + data + zero pad to 8-byte block.
func jdpayPad(data []byte) []byte {
	n := len(data)
	out := make([]byte, 0, n+8)
	out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	out = append(out, data...)
	add := 8 - (n+4)%8
	if add == 8 {
		add = 0
	}
	for i := 0; i < add; i++ {
		out = append(out, 0)
	}
	return out
}

func jdpayUnpad(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("short block")
	}
	n := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if n < 0 || 4+n > len(data) {
		return "", errors.New("bad length prefix")
	}
	return string(data[4 : 4+n]), nil
}

func jdpayDESECB(key, data []byte, encrypt bool) ([]byte, error) {
	block, e := des.NewTripleDESCipher(key)
	if e != nil {
		return nil, e
	}
	bs := block.BlockSize()
	if len(data)%bs != 0 {
		return nil, errors.New("3des: input not block-aligned")
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += bs {
		if encrypt {
			block.Encrypt(out[i:i+bs], data[i:i+bs])
		} else {
			block.Decrypt(out[i:i+bs], data[i:i+bs])
		}
	}
	return out, nil
}

// jdpayNormalizeDESKey accepts a 24-byte key, or expands a 16-byte key to
// (K1,K2,K1) as OpenSSL des-ede3 does.
func jdpayNormalizeDESKey(k []byte) ([]byte, error) {
	switch len(k) {
	case 24:
		return k, nil
	case 16:
		out := make([]byte, 24)
		copy(out, k)
		copy(out[16:], k[:8])
		return out, nil
	}
	return nil, errors.New("3des: key must be 16 or 24 bytes")
}

// ---- RSA raw private-encrypt / public-decrypt (PKCS#1 v1.5, no DigestInfo) ----

// jdpayPrivateEncrypt == PHP openssl_private_encrypt(data, OPENSSL_PKCS1_PADDING):
// EMSA-PKCS1 type-1 padded block raised to d mod n.
func jdpayPrivateEncrypt(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	k := (key.N.BitLen() + 7) / 8
	if len(data) > k-11 {
		return nil, errors.New("rsa: data too long")
	}
	em := make([]byte, k)
	em[0] = 0x00
	em[1] = 0x01
	padLen := k - len(data) - 3
	for i := 0; i < padLen; i++ {
		em[2+i] = 0xff
	}
	em[2+padLen] = 0x00
	copy(em[3+padLen:], data)
	m := new(big.Int).SetBytes(em)
	c := new(big.Int).Exp(m, key.D, key.N)
	return jdpayLeftPad(c.Bytes(), k), nil
}

// jdpayPublicDecrypt == PHP openssl_public_decrypt(data, OPENSSL_PKCS1_PADDING):
// c^e mod n, then strip a type-1 (0x00 0x01 0xff.. 0x00) block.
func jdpayPublicDecrypt(key *rsa.PublicKey, sig []byte) ([]byte, error) {
	k := (key.N.BitLen() + 7) / 8
	if len(sig) > k {
		return nil, errors.New("rsa: signature too long")
	}
	c := new(big.Int).SetBytes(sig)
	m := new(big.Int).Exp(c, big.NewInt(int64(key.E)), key.N)
	em := jdpayLeftPad(m.Bytes(), k)
	if len(em) < 11 || em[0] != 0x00 || em[1] != 0x01 {
		return nil, errors.New("rsa: bad padding")
	}
	i := 2
	for ; i < len(em); i++ {
		if em[i] == 0x00 {
			break
		}
		if em[i] != 0xff {
			return nil, errors.New("rsa: bad PS")
		}
	}
	if i == len(em) {
		return nil, errors.New("rsa: no separator")
	}
	return em[i+1:], nil
}

func jdpayLeftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}

func jdpayParsePlatformKey() (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(jdpayPlatformPublicKeyPEM))
	if block == nil {
		return nil, errors.New("bad platform pem")
	}
	parsed, e := x509.ParsePKIXPublicKey(block.Bytes)
	if e != nil {
		return nil, e
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not rsa")
	}
	return key, nil
}

// ---- XML helpers ----

func jdpaySignString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// jdpayOrderedKeys preserves the original insertion order used by the refund
// request (version, merchant, tradeNum, oTradeNum, amount, currency); the
// sign digest covers this exact serialization, so order must be stable.
func jdpayOrderedKeys(param map[string]string) []string {
	order := []string{"version", "merchant", "tradeNum", "oTradeNum", "amount", "currency"}
	out := make([]string, 0, len(param))
	for _, k := range order {
		if _, ok := param[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

func jdpayBuildXML(root string, keys []string, m map[string]string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?><" + root + ">")
	for _, k := range keys {
		b.WriteString("<" + k + ">" + jdpayXMLEscape(m[k]) + "</" + k + ">")
	}
	b.WriteString("</" + root + ">")
	return b.String()
}

func jdpayXMLEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// jdpayParseFlat reads a flat one-level XML document into a string map.
func jdpayParseFlat(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	d := xml.NewDecoder(strings.NewReader(string(raw)))
	out := map[string]string{}
	depth, key := 0, ""
	var text strings.Builder
	for {
		t, e := d.Token()
		if e != nil {
			if e.Error() == "EOF" {
				break
			}
			return nil, ErrInvalid
		}
		switch v := t.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				key = v.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if depth == 2 {
				text.Write(v)
			}
		case xml.EndElement:
			if depth == 2 {
				out[key] = text.String()
			}
			depth--
		}
	}
	return out, nil
}

func jdpayCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 48 || strings.ContainsAny(code, " \r\n") {
		return "REJECTED"
	}
	return code
}

var _ Provider = (*jdpayPlugin)(nil)
var _ NotificationVerifier = (*jdpayPlugin)(nil)
var _ CreateValidator = (*jdpayPlugin)(nil)
