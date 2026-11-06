package kyc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	domain "goepay/internal/kyc"
	"net/http"
	"net/url"
	"strings"
)

var documents = map[string]struct{ action, path string }{
	"idcard": {"RecognizeIdcard", "idcard"}, "idcard_back": {"RecognizeIdcard", "idcard"}, "passport": {"RecognizePassport", "overseas_passport"}, "bank_card": {"RecognizeBankCard", "bankcard"}, "business_license": {"RecognizeBusinessLicense", "business_license"}, "bank_account_license": {"RecognizeBankAccountLicense", "account_opening"},
}

func (p *Adapter) Recognize(ctx context.Context, s domain.Snapshot, kind string, image []byte) (domain.OCRResult, error) {
	if _, ok := documents[kind]; !ok || len(image) == 0 || len(image) > 4<<20 || s.OCRType == "baidu" && len(image) > 2<<20 {
		return domain.OCRResult{}, domain.ErrInvalid
	}
	// Baidu limits the base64 plus form-escaped image to 4 MiB, which is
	// stricter than the raw byte limit for images with many '/' or '+' bytes.
	if s.OCRType == "baidu" && len(url.QueryEscape(base64.StdEncoding.EncodeToString(image))) > 4<<20 {
		return domain.OCRResult{}, domain.ErrInvalid
	}
	var fields map[string]string
	var e error
	switch s.OCRType {
	case "aliyun":
		fields, e = p.aliyunOCR(ctx, s.Credentials, kind, image)
	case "baidu":
		fields, e = p.baiduOCR(ctx, s.Credentials, kind, image)
	default:
		return domain.OCRResult{}, domain.ErrUnavailable
	}
	if e != nil {
		return domain.OCRResult{}, e
	}
	has := false
	for k, v := range fields {
		if len(v) > 8192 || strings.ContainsAny(v, "\x00\r") {
			return domain.OCRResult{}, domain.ErrUnknown
		}
		fields[k] = strings.TrimSpace(v)
		has = has || fields[k] != ""
	}
	if !has {
		return domain.OCRResult{}, domain.ErrUnknown
	}
	return domain.OCRResult{Fields: fields}, nil
}
func (p *Adapter) aliyunOCR(ctx context.Context, c domain.Credentials, kind string, image []byte) (map[string]string, error) {
	if !value(c.OCRAliyunID, 256) || !value(c.OCRAliyunKey, 4096) {
		return nil, domain.ErrUnavailable
	}
	q := rpcParams(documents[kind].action, "2021-07-07", c.OCRAliyunID, c.OCRAliyunKey)
	q.Set("Signature", rpcSignature(q, c.OCRAliyunKey))
	m, e := p.request(ctx, http.MethodPost, "https://ocr-api.cn-hangzhou.aliyuncs.com/?"+q.Encode(), image, map[string]string{"Content-Type": "application/octet-stream"})
	if e != nil {
		return nil, e
	}
	raw := str(m, "Data")
	parsed, e := object([]byte(raw))
	if e != nil {
		return nil, e
	}
	d, e := child(parsed, "data")
	if e != nil {
		return nil, e
	}
	if kind == "idcard" || kind == "idcard_back" {
		side := "face"
		if kind == "idcard_back" {
			side = "back"
		}
		d, e = child(d, side)
		if e != nil {
			return nil, e
		}
		d, e = child(d, "data")
		if e != nil {
			return nil, e
		}
	}
	take := func(mapping map[string]string) map[string]string {
		out := map[string]string{}
		for dest, source := range mapping {
			out[dest] = str(d, source)
		}
		return out
	}
	var out map[string]string
	switch kind {
	case "idcard":
		out = take(map[string]string{"id_no": "idNumber", "name": "name", "address": "address", "sex": "sex", "ethnicity": "ethnicity", "birth_date": "birthDate"})
		out["birth_date"] = date(out["birth_date"])
	case "idcard_back":
		out = take(map[string]string{"issue_authority": "issueAuthority"})
		v := strings.SplitN(str(d, "validPeriod"), "-", 2)
		out["period_begin"] = date(v[0])
		out["period_end"] = ""
		if len(v) == 2 {
			out["period_end"] = date(v[1])
		}
	case "passport":
		out = take(map[string]string{"passport_no": "passportNumber", "passport_type": "passportType", "last_name": "surname", "first_name": "givenName", "name": "name", "sex": "sex", "country": "country", "birth_date": "birthDateYmd", "period_begin": "issueDateYmd", "period_end": "validToDate"})
		for _, k := range []string{"birth_date", "period_begin", "period_end"} {
			out[k] = date(out[k])
		}
	case "bank_card":
		out = take(map[string]string{"bank_name": "bankName", "card_type": "cardType", "card_no": "cardNumber", "period_end": "validToDate"})
		out["card_no"] = strings.ReplaceAll(out["card_no"], " ", "")
	case "business_license":
		out = take(map[string]string{"license_no": "creditCode", "name": "companyName", "address": "businessAddress", "reg_date": "RegistrationDate", "legal_name": "legalPerson", "type": "companyType", "registered_capital": "registeredCapital", "business_scope": "businessScope", "period_begin": "validFromDate", "period_end": "validToDate"})
		for _, k := range []string{"reg_date", "period_begin", "period_end"} {
			out[k] = date(out[k])
		}
		if out["period_end"] == "" {
			out["period_end"] = "长期"
		}
	case "bank_account_license":
		out = take(map[string]string{"bank_account": "bankAccount", "legal_name": "legalRepresentative", "bank_name": "depositaryBank", "approval_no": "approvalNumber", "name": "customerName", "permit_no": "permitNumber"})
	}
	return out, nil
}
func (p *Adapter) baiduOCR(ctx context.Context, c domain.Credentials, kind string, image []byte) (map[string]string, error) {
	if !value(c.OCRBaiduID, 256) || !value(c.OCRBaiduKey, 4096) {
		return nil, domain.ErrUnavailable
	}
	// No constructor I/O, hidden OCR retries or process-global token cache shared
	// across credential revisions. A claim covers this one token+OCR exchange.
	token, e := p.request(ctx, http.MethodPost, "https://aip.baidubce.com/oauth/2.0/token", []byte((url.Values{"grant_type": {"client_credentials"}, "client_id": {c.OCRBaiduID}, "client_secret": {c.OCRBaiduKey}}).Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if e != nil {
		return nil, e
	}
	access := str(token, "access_token")
	if !value(access, 4096) {
		return nil, domain.ErrUnavailable
	}
	form := url.Values{"image": {base64.StdEncoding.EncodeToString(image)}}
	if kind == "idcard" {
		form.Set("id_card_side", "front")
	} else if kind == "idcard_back" {
		form.Set("id_card_side", "back")
	}
	m, e := p.request(ctx, http.MethodPost, "https://aip.baidubce.com/rest/2.0/ocr/v1/"+documents[kind].path+"?"+(url.Values{"access_token": {access}}).Encode(), []byte(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if e != nil {
		return nil, e
	}
	if _, ok := m["error_code"]; ok {
		return nil, domain.ErrUnavailable
	}
	if kind == "bank_card" {
		d, e := child(m, "result")
		if e != nil {
			return nil, e
		}
		n, _ := number(d["bank_card_type"])
		types := map[int64]string{1: "DC", 2: "CC", 3: "SCC", 4: "PC"}
		return map[string]string{"bank_name": str(d, "bank_name"), "card_type": types[n], "card_no": strings.ReplaceAll(str(d, "bank_card_number"), " ", ""), "period_end": str(d, "valid_date")}, nil
	}
	if kind == "idcard" || kind == "idcard_back" {
		if str(m, "image_status") != "normal" {
			return nil, domain.ErrInvalid
		}
	}
	words, e := child(m, "words_result")
	if e != nil {
		return nil, e
	}
	word := func(k string) string {
		d, e := child(words, k)
		if e != nil {
			return ""
		}
		s := str(d, "words")
		if s != "" {
			return s
		}
		var list []string
		if json.Unmarshal(d["word"], &list) == nil && len(list) > 0 {
			return list[0]
		}
		return ""
	}
	take := func(mapping map[string]string) map[string]string {
		out := map[string]string{}
		for dest, source := range mapping {
			out[dest] = word(source)
		}
		return out
	}
	var out map[string]string
	switch kind {
	case "idcard":
		out = take(map[string]string{"id_no": "公民身份号码", "name": "姓名", "address": "住址", "sex": "性别", "ethnicity": "民族", "birth_date": "出生"})
		out["birth_date"] = date(out["birth_date"])
	case "idcard_back":
		out = take(map[string]string{"issue_authority": "签发机关", "period_begin": "签发日期", "period_end": "失效日期"})
		out["period_begin"] = date(out["period_begin"])
		out["period_end"] = date(out["period_end"])
	case "passport":
		out = take(map[string]string{"passport_no": "护照号", "name": "姓名拼音", "sex": "性别", "country": "国籍", "birth_date": "出生日期", "period_end": "有效期"})
		out["birth_date"] = date(out["birth_date"])
		out["period_end"] = date(out["period_end"])
	case "business_license":
		out = take(map[string]string{"license_no": "社会信用代码", "name": "单位名称", "address": "地址", "reg_date": "成立日期", "legal_name": "法人", "type": "类型", "registered_capital": "注册资本", "business_scope": "经营范围", "period_begin": "有效期起始日期", "period_end": "有效期"})
		if blank(out["license_no"]) {
			out["license_no"] = word("证件编号")
		}
		if blank(out["period_begin"]) {
			out["period_begin"] = word("核准日期")
		}
		if blank(out["period_end"]) {
			out["period_end"] = "长期"
		}
		for _, k := range []string{"reg_date", "period_begin", "period_end"} {
			out[k] = date(out[k])
		}
	case "bank_account_license":
		out = take(map[string]string{"bank_account": "账号", "legal_name": "法人", "bank_name": "开户银行", "approval_no": "编号", "name": "公司名称", "permit_no": "核准号"})
	}
	return out, nil
}
func blank(s string) bool { return s == "" || s == "无" || s == "年月日" }
func date(s string) string {
	if blank(s) {
		return ""
	}
	if s == "长期" || s == "永久" {
		return s
	}
	s = strings.NewReplacer("年", "-", "月", "-", "日", "", ".", "-", "/", "-").Replace(s)
	if len(s) == 8 && !strings.Contains(s, "-") {
		s = s[:4] + "-" + s[4:6] + "-" + s[6:]
	}
	return s
}
