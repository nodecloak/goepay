package kyc233

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	domain "goepay/internal/kyc233"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (p *Adapter) phone(ctx context.Context, c domain.Credentials, in domain.Submission) (bool, error) {
	if !value(c.AppCode, 4096) {
		return false, domain.ErrUnavailable
	}
	q := url.Values{"idcard": {in.Number}, "realname": {in.Name}, "phone": {in.Phone}}
	m, e := p.request(ctx, http.MethodGet, "https://phone3.market.alicloudapi.com/phonethree?"+q.Encode(), nil, map[string]string{"Authorization": "APPCODE " + c.AppCode})
	if e != nil {
		return false, e
	}
	n, ok := number(m["code"])
	if !ok {
		return false, domain.ErrUnknown
	}
	if n != 200 {
		return false, domain.ErrUnavailable
	}
	return true, nil
}
func (p *Adapter) company(ctx context.Context, c domain.Credentials, in domain.Submission) (bool, error) {
	if !value(c.CompanyAppCode, 4096) {
		return false, domain.ErrUnavailable
	}
	q := url.Values{"companyName": {in.CompanyName}, "creditNo": {in.CompanyNumber}, "legalPerson": {in.Name}}
	m, e := p.request(ctx, http.MethodGet, "https://companythree.shumaidata.com/companythree/check?"+q.Encode(), nil, map[string]string{"Authorization": "APPCODE " + c.CompanyAppCode})
	if e != nil {
		return false, e
	}
	n, ok := number(m["code"])
	if !ok || n != 200 {
		return false, domain.ErrUnavailable
	}
	d, e := child(m, "data")
	if e != nil {
		return false, e
	}
	n, ok = number(d["result"])
	if !ok {
		return false, domain.ErrUnknown
	}
	return n == 0, nil
}
func percent(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
func rpcSignature(q url.Values, key string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, percent(k)+"="+percent(q.Get(k)))
	}
	mac := hmac.New(sha1.New, []byte(key+"&"))
	mac.Write([]byte("POST&%2F&" + percent(strings.Join(parts, "&"))))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func rpcParams(action, version, id, key string) url.Values {
	q := url.Values{"Action": {action}, "Format": {"JSON"}, "Version": {version}, "AccessKeyId": {id}, "SignatureMethod": {"HMAC-SHA1"}, "Timestamp": {time.Now().UTC().Format("2006-01-02T15:04:05Z")}, "SignatureVersion": {"1.0"}, "SignatureNonce": {nonce()}}
	return q
}
func (p *Adapter) financial(ctx context.Context, c domain.Credentials, params map[string]string) (map[string]json.RawMessage, error) {
	if !value(c.AliyunID, 256) || !value(c.AliyunKey, 4096) || !value(c.AliyunScene, 128) {
		return nil, domain.ErrUnavailable
	}
	raw, _ := json.Marshal(params)
	q := rpcParams("ExecuteRequest", "2017-03-31", c.AliyunID, c.AliyunKey)
	q.Set("Service", "fin_face_verify")
	q.Set("ServiceParameters", string(raw))
	q.Set("Signature", rpcSignature(q, c.AliyunKey))
	m, e := p.request(ctx, http.MethodPost, "https://saf.cn-shanghai.aliyuncs.com/", []byte(q.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if e != nil {
		return nil, e
	}
	n, ok := number(alias(m, "Code", "code"))
	if !ok || n != 200 {
		return nil, domain.ErrUnavailable
	}
	return object(alias(m, "Data", "data"))
}
func (p *Adapter) aliyunInit(ctx context.Context, c domain.Credentials, in domain.Submission, a domain.Attempt) (domain.InitResult, error) {
	d, e := p.financial(ctx, c, map[string]string{"method": "init", "sceneId": c.AliyunScene, "outerOrderNo": a.ID, "bizCode": "FACE", "identityType": "CERT_INFO", "certType": "IDENTITY_CARD", "certNo": in.Number, "certName": in.Name, "returnUrl": a.CallbackURL})
	if e != nil {
		return domain.InitResult{}, e
	}
	token, link := str(d, "certifyId"), str(d, "certifyUrl")
	if !value(token, 4096) || !providerURL(5, link) {
		return domain.InitResult{}, domain.ErrUnknown
	}
	return domain.InitResult{Token: token, URL: link, Scan: "alipay", Status: "pending"}, nil
}
func (p *Adapter) aliyunQuery(ctx context.Context, c domain.Credentials, a domain.Attempt) (domain.QueryResult, error) {
	d, e := p.financial(ctx, c, map[string]string{"method": "query", "sceneId": c.AliyunScene, "certifyId": a.Token})
	if e != nil {
		return domain.QueryResult{}, e
	}
	switch str(d, "passed") {
	case "T":
		return domain.QueryResult{Status: "passed"}, nil
	case "F": // Alibaba documents F for both failure and not submitted; keep resumable.
		return domain.QueryResult{Status: "pending"}, nil
	default:
		return domain.QueryResult{}, domain.ErrUnknown
	}
}
func hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func hmac256(k []byte, s string) []byte {
	h := hmac.New(sha256.New, k)
	h.Write([]byte(s))
	return h.Sum(nil)
}
func (p *Adapter) tencent(ctx context.Context, c domain.Credentials, action string, params map[string]string) (map[string]json.RawMessage, error) {
	if !value(c.TencentID, 256) || !value(c.TencentKey, 4096) || !value(c.TencentRuleID, 128) {
		return nil, domain.ErrUnavailable
	}
	raw, _ := json.Marshal(params)
	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	stamp := strconv.FormatInt(now.Unix(), 10)
	canonical := "POST\n/\n\ncontent-type:application/json; charset=utf-8\nhost:faceid.tencentcloudapi.com\n\ncontent-type;host\n" + hash(string(raw))
	scope := date + "/faceid/tc3_request"
	toSign := "TC3-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hash(canonical)
	key := hmac256(hmac256(hmac256([]byte("TC3"+c.TencentKey), date), "faceid"), "tc3_request")
	headers := map[string]string{"Content-Type": "application/json; charset=utf-8", "X-TC-Action": action, "X-TC-Timestamp": stamp, "X-TC-Version": "2018-03-01", "Authorization": "TC3-HMAC-SHA256 Credential=" + c.TencentID + "/" + scope + ", SignedHeaders=content-type;host, Signature=" + hex.EncodeToString(hmac256(key, toSign))}
	m, e := p.request(ctx, http.MethodPost, "https://faceid.tencentcloudapi.com/", raw, headers)
	if e != nil {
		return nil, e
	}
	d, e := child(m, "Response")
	if e != nil {
		return nil, e
	}
	if _, ok := d["Error"]; ok {
		return nil, domain.ErrUnavailable
	}
	return d, nil
}
func (p *Adapter) tencentInit(ctx context.Context, c domain.Credentials, in domain.Submission, a domain.Attempt) (domain.InitResult, error) {
	d, e := p.tencent(ctx, c, "DetectAuth", map[string]string{"RuleId": c.TencentRuleID, "Name": in.Name, "IdCard": in.Number, "RedirectUrl": a.CallbackURL, "Extra": a.ID})
	if e != nil {
		return domain.InitResult{}, e
	}
	token, link := str(d, "BizToken"), str(d, "Url")
	if !value(token, 4096) || !providerURL(4, link) {
		return domain.InitResult{}, domain.ErrUnknown
	}
	// The domain seals this opaque token. Keep only hashes of the expected
	// identity here so a RuleId allowing edits cannot certify another person.
	sealedInput, _ := json.Marshal(map[string]string{"biz_token": token, "name_hash": hash(in.Name), "number_hash": hash(in.Number), "attempt_id": a.ID})
	return domain.InitResult{Token: string(sealedInput), URL: link, Scan: "wechat", Status: "pending"}, nil
}
func (p *Adapter) tencentQuery(ctx context.Context, c domain.Credentials, a domain.Attempt) (domain.QueryResult, error) {
	bound, e := object([]byte(a.Token))
	if e != nil || str(bound, "attempt_id") != a.ID || !value(str(bound, "biz_token"), 4096) || len(str(bound, "name_hash")) != 64 || len(str(bound, "number_hash")) != 64 {
		return domain.QueryResult{}, domain.ErrInvalid
	}
	d, e := p.tencent(ctx, c, "GetDetectInfoEnhanced", map[string]string{"RuleId": c.TencentRuleID, "BizToken": str(bound, "biz_token"), "InfoType": "1"})
	if e != nil {
		return domain.QueryResult{}, e
	}
	if string(d["Text"]) == "null" {
		return domain.QueryResult{Status: "pending"}, nil
	}
	text, e := child(d, "Text")
	if e != nil {
		return domain.QueryResult{}, e
	}
	n, ok := number(text["ErrCode"])
	if !ok {
		return domain.QueryResult{}, domain.ErrUnknown
	}
	if extra := str(text, "Extra"); extra != "" && extra != a.ID {
		return domain.QueryResult{}, domain.ErrUnknown
	}
	// A RuleId requiring intention verification needs separate final proof.
	var intention bool
	if json.Unmarshal(d["IsVerifyIntention"], &intention) == nil && intention {
		return domain.QueryResult{}, domain.ErrUnavailable
	}
	status := "rejected"
	if n == 0 {
		if hash(str(text, "Name")) != str(bound, "name_hash") || hash(str(text, "IdCard")) != str(bound, "number_hash") {
			return domain.QueryResult{}, domain.ErrUnknown
		}
		status = "passed"
	}
	return domain.QueryResult{Status: status}, nil
}
