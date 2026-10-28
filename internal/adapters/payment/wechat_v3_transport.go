package payment

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Keys are supplied by trusted configuration. Unknown platform serials never
// trigger network key discovery in the payment request path.
type WechatV3Config struct {
	EnableChannelSharing              bool
	EncryptionSerial                  string
	AppID, MerchantID, MerchantSerial string
	PrivateKey                        *rsa.PrivateKey
	APIv3Key                          []byte
	VerificationKeys                  map[string]*rsa.PublicKey
	HTTPClient                        *http.Client
	Methods                           []string
	MiniProgram                       *MiniProgramConfig
	H5SiteURL, H5SiteName             string
	// MerchantTransfer reads the site switch 微信V3转账产品类型 = 商家转账（新）.
	// Nil means the channel never carries merchant-transfer bills.
	MerchantTransfer func() bool
}
type wechatV3Client struct{ config WechatV3Config }

var wechatV3Identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func newWechatV3Client(c WechatV3Config) (*wechatV3Client, error) {
	if !wechatV3Identifier.MatchString(c.AppID) || len(c.AppID) > 32 || !wechatV3Identifier.MatchString(c.MerchantID) || len(c.MerchantID) > 32 || !wechatV3Identifier.MatchString(c.MerchantSerial) || len(c.APIv3Key) != 32 || c.PrivateKey == nil || c.PrivateKey.N == nil || c.PrivateKey.N.BitLen() < 2048 || c.PrivateKey.Validate() != nil || len(c.VerificationKeys) == 0 || len(c.VerificationKeys) > 8 {
		return nil, ErrInvalid
	}
	private, e := x509.ParsePKCS1PrivateKey(x509.MarshalPKCS1PrivateKey(c.PrivateKey))
	if e != nil {
		return nil, ErrInvalid
	}
	c.PrivateKey = private
	keys := map[string]*rsa.PublicKey{}
	for serial, key := range c.VerificationKeys {
		if !wechatV3Identifier.MatchString(serial) || key == nil || key.N == nil || key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
			return nil, ErrInvalid
		}
		keys[serial] = &rsa.PublicKey{N: new(big.Int).Set(key.N), E: key.E}
	}
	c.VerificationKeys = keys
	if c.EncryptionSerial == "" && len(keys) == 1 {
		for serial := range keys {
			c.EncryptionSerial = serial
		}
	}
	if c.EncryptionSerial != "" && keys[c.EncryptionSerial] == nil || c.EnableChannelSharing && c.EncryptionSerial == "" {
		return nil, ErrInvalid
	}
	c.APIv3Key = append([]byte{}, c.APIv3Key...)
	c.Methods = append([]string{}, c.Methods...)
	c.HTTPClient = boundedClient(c.HTTPClient)
	return &wechatV3Client{config: c}, nil
}
func (c *wechatV3Client) call(ctx context.Context, method, target string, input any) ([]byte, error) {
	if method != "GET" && method != "POST" {
		return nil, ErrInvalid
	}
	u, e := url.ParseRequestURI(target)
	if e != nil || !strings.HasPrefix(target, "/v3/") || u.IsAbs() || u.Fragment != "" || u.Host != "" || strings.ContainsAny(target, "\r\n\x00") {
		return nil, ErrInvalid
	}
	var body []byte
	if input != nil {
		if method == "GET" {
			return nil, ErrInvalid
		}
		body, e = json.Marshal(input)
		if e != nil || len(body) > 1<<20 {
			return nil, ErrInvalid
		}
	}
	n, e := nonce()
	if e != nil {
		return nil, e
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature, e := SignWechatV3Request(method, target, stamp, n, body, c.config.PrivateKey)
	if e != nil {
		return nil, e
	}
	request, e := http.NewRequestWithContext(ctx, method, "https://api.mch.weixin.qq.com"+target, bytes.NewReader(body))
	if e != nil {
		return nil, ErrInvalid
	}
	request.Header.Set("Authorization", fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`, c.config.MerchantID, n, stamp, c.config.MerchantSerial, signature))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "GOEPAY/1")
	if c.config.EncryptionSerial != "" {
		request.Header.Set("Wechatpay-Serial", c.config.EncryptionSerial)
	}
	fail := func(cause error) ([]byte, error) {
		if method == "POST" {
			return nil, &UnknownOutcome{Operation: target, Cause: cause}
		}
		return nil, cause
	}
	response, e := c.config.HTTPClient.Do(request)
	if e != nil {
		return fail(ErrAuthentication)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return fail(ErrAuthentication)
	}
	for _, name := range []string{"Wechatpay-Timestamp", "Wechatpay-Nonce", "Wechatpay-Serial", "Wechatpay-Signature"} {
		if len(response.Header.Get(name)) > 1024 {
			return fail(ErrAuthentication)
		}
	}
	if e = VerifyWechatV3Message(response.Header, raw, c.config.VerificationKeys, time.Now(), 5*time.Minute); e != nil {
		return fail(ErrAuthentication)
	}
	if response.StatusCode == 204 && len(raw) == 0 {
		return raw, nil
	}
	content, _, e := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if e != nil || content != "application/json" || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' || providerUniqueJSON(raw) != nil {
		return fail(ErrAuthentication)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &problem) != nil {
			return fail(ErrAuthentication)
		}
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			switch problem.Code {
			case "PARAM_ERROR", "LACK_PARAMS", "APPID_MCHID_NOT_MATCH", "NO_AUTH":
				return nil, &ProviderError{Code: problem.Code}
			}
		}
		return fail(ErrAuthentication)
	}
	if response.StatusCode != 200 && response.StatusCode != 201 && response.StatusCode != 202 {
		return fail(ErrAuthentication)
	}
	return raw, nil
}
