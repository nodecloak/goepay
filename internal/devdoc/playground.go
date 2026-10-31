package devdoc

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"goepay/internal/compat"
	"goepay/internal/ledger"
)

const playgroundPath = "/doc/playground/run"
const playgroundBodyLimit = 32 * 1024
const playgroundMerchant = "900001"

var playgroundSlots = make(chan struct{}, 8)

type playgroundInput struct {
	Endpoint  string
	Params    compat.Params
	PublicKey string
}
type playgroundRequestInfo struct {
	Method            string `json:"method"`
	Path              string `json:"path"`
	Canonical         string `json:"canonical"`
	SignatureVerified bool   `json:"signature_verified"`
}
type playgroundEnvelope struct {
	Environment       string                 `json:"environment"`
	Request           *playgroundRequestInfo `json:"request,omitempty"`
	Response          map[string]any         `json:"response,omitempty"`
	PlatformPublicKey string                 `json:"platform_public_key,omitempty"`
	ElapsedMS         float64                `json:"elapsed_ms"`
	Error             string                 `json:"error,omitempty"`
}

// The playground has no account store, job queue, provider, configurable URL or
// network client. Its only private key is ephemeral process memory dedicated to
// signed examples; no production key or environment value is ever loaded.
var playgroundPlatform struct {
	sync.Once
	private *rsa.PrivateKey
	public  string
	err     error
}

func playgroundSigningKey() (*rsa.PrivateKey, string, error) {
	playgroundPlatform.Do(func() {
		playgroundPlatform.private, playgroundPlatform.err = rsa.GenerateKey(rand.Reader, 2048)
		if playgroundPlatform.err != nil {
			return
		}
		der, err := x509.MarshalPKIXPublicKey(&playgroundPlatform.private.PublicKey)
		if err != nil {
			playgroundPlatform.err = err
			return
		}
		playgroundPlatform.public = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	})
	return playgroundPlatform.private, playgroundPlatform.public, playgroundPlatform.err
}

func playgroundOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) != 1 {
		return false
	}
	u, err := url.Parse(values[0])
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	// TLS may terminate at the site's reverse proxy. No forwarded-protocol
	// header is trusted here; cleartext is allowed only for literal loopback.
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && ip != nil && ip.IsLoopback()
}

func uniqueJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		keyToken, e := d.Token()
		key, ok := keyToken.(string)
		if e != nil || !ok {
			return nil, errors.New("invalid object key")
		}
		if _, duplicate := out[key]; duplicate {
			return nil, errors.New("duplicate object key")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid object close")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return out, nil
}

func decodePlayground(raw []byte) (playgroundInput, error) {
	var result playgroundInput
	if !utf8.Valid(raw) {
		return result, errors.New("请求必须为 UTF-8 JSON")
	}
	object, err := uniqueJSONObject(raw)
	if err != nil {
		return result, errors.New("请求必须为无重复字段的 JSON 对象")
	}
	if len(object) != 3 {
		return result, errors.New("仅接受 endpoint、params 和 public_key")
	}
	for key := range object {
		if key != "endpoint" && key != "params" && key != "public_key" {
			return result, errors.New("请求包含不支持的字段")
		}
	}
	if json.Unmarshal(object["endpoint"], &result.Endpoint) != nil || json.Unmarshal(object["public_key"], &result.PublicKey) != nil || len(result.PublicKey) > 2048 {
		return result, errors.New("接口或临时公钥格式不正确")
	}
	fields, err := uniqueJSONObject(object["params"])
	if err != nil || len(fields) > 64 {
		return result, errors.New("params 必须为无重复字段的标量对象")
	}
	values := url.Values{}
	for key, rawValue := range fields {
		var value string
		if len(key) > 64 || json.Unmarshal(rawValue, &value) != nil || len(value) > 4096 || bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) {
			return result, errors.New("params 只接受字符串值")
		}
		values.Set(key, value)
	}
	result.Params, err = compat.Parse(values)
	if err != nil {
		return result, errors.New("参数名或重复值不符合唯一标量规则")
	}
	return result, nil
}

// SandboxServe executes an anonymous, stateless protocol demonstration. It does
// not dispatch to the payment API or persist any simulated result. Same-origin
// POST JSON and the fixed endpoint set are the complete network boundary.
func SandboxServe(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != playgroundPath {
		return false
	}
	start := time.Now()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	write := func(status int, envelope playgroundEnvelope) {
		envelope.Environment = "documentation-sandbox"
		envelope.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(envelope)
	}
	fail := func(status int, message string) { write(status, playgroundEnvelope{Error: message}) }
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(405, "文档沙箱仅接受 POST")
		return true
	}
	if !playgroundOrigin(r) {
		fail(403, "请从本站文档面板发送沙箱请求")
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(415, "请使用 application/json 请求文档沙箱")
		return true
	}
	select {
	case playgroundSlots <- struct{}{}:
		defer func() { <-playgroundSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		fail(429, "文档沙箱繁忙，请稍后重试")
		return true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, playgroundBodyLimit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(413, "文档沙箱请求不得超过 32 KiB")
		} else {
			fail(400, "无法读取沙箱请求")
		}
		return true
	}
	in, err := decodePlayground(raw)
	if err != nil {
		fail(400, err.Error())
		return true
	}
	spec := EndpointForSlug(in.Endpoint)
	if spec == nil {
		fail(400, "请选择文档中列出的 V2 接口")
		return true
	}
	public, err := compat.ParsePublicKey(in.PublicKey)
	if err != nil || public.N.BitLen() != 2048 {
		fail(400, "请使用文档面板生成的临时 2048 位 RSA 公钥")
		return true
	}
	if err = compat.Verify(in.Params, compat.Credentials{Algorithm: "RSA", PublicKey: public, Profile: compat.ProfileProServer, TimestampWindow: 300 * time.Second}, time.Now()); err != nil {
		fail(400, "请求签名或时间戳校验失败，请重新生成沙箱请求")
		return true
	}
	key, pub, err := playgroundSigningKey()
	if err != nil {
		fail(503, "文档沙箱签名暂不可用，请稍后重试")
		return true
	}
	response := playgroundResponse(*spec, in.Params)
	response["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	response["sign_type"] = "RSA"
	projection := compat.Params{}
	for k, value := range response {
		switch v := value.(type) {
		case []any, []map[string]any, map[string]any:
			continue
		case bool:
			if v {
				projection[k] = "1"
			} else {
				projection[k] = ""
			}
		case nil:
			projection[k] = ""
		default:
			projection[k] = fmt.Sprint(value)
		}
	}
	response["sign"], err = compat.SignRSAWithProfile(projection, key, compat.ProfileSDKV2)
	if err != nil {
		fail(503, "文档沙箱签名暂不可用，请稍后重试")
		return true
	}
	write(200, playgroundEnvelope{Request: &playgroundRequestInfo{Method: spec.Method, Path: spec.Path, Canonical: compat.Canonical(in.Params), SignatureVerified: true}, Response: response, PlatformPublicKey: pub})
	return true
}

var playgroundOrderNumber = regexp.MustCompile(`^[A-Za-z0-9._|\-]{1,128}$`)

func playgroundFailure(message string) map[string]any {
	return map[string]any{"code": -1, "msg": message}
}

func playgroundResponse(spec EndpointSpec, p compat.Params) map[string]any {
	if p["pid"] != playgroundMerchant {
		return playgroundFailure("文档沙箱仅接受临时商户 900001")
	}
	for _, f := range spec.Fields {
		if f.Required && strings.TrimSpace(p[f.Name]) == "" {
			return playgroundFailure("缺少参数 " + f.Name)
		}
	}
	for _, name := range []string{"out_trade_no", "out_refund_no", "out_biz_no"} {
		if p[name] != "" && !playgroundOrderNumber.MatchString(p[name]) {
			return playgroundFailure(name + " 只能使用最多 128 字节的字母、数字和 . _ - |")
		}
	}
	for _, name := range []string{"notify_url", "return_url"} {
		if p[name] != "" {
			u, e := url.Parse(p[name])
			if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
				return playgroundFailure(name + " 地址格式不正确")
			}
		}
	}
	amount := int64(0)
	if p["money"] != "" {
		var err error
		amount, err = ledger.Parse(p["money"])
		if err != nil || amount <= 0 {
			return playgroundFailure("money 必须为正的十进制元字符串，最多两位小数")
		}
	}
	switch spec.Slug {
	case "pay_submit", "pay_create":
		if p["type"] != "alipay" && p["type"] != "wxpay" && !(spec.Slug == "pay_submit" && p["type"] == "") {
			return playgroundFailure("文档沙箱请选择 alipay 或 wxpay")
		}
		name := p["name"]
		for len(name) > 127 {
			name = name[:len(name)-1]
			for !utf8.ValidString(name) {
				name = name[:len(name)-1]
			}
		}
		digest := sha256.Sum256([]byte(p["out_trade_no"]))
		trade := "DOC_" + strings.ToUpper(hex.EncodeToString(digest[:6]))
		if spec.Slug == "pay_submit" {
			location := "/checkout/" + trade
			if p["type"] == "" {
				location = "/cashier?intent=DOCUMENTATION_SANDBOX_ONLY"
			}
			return map[string]any{"code": 0, "http_status": 302, "location": location, "trade_no": trade, "out_trade_no": p["out_trade_no"], "msg": "沙箱仅展示跳转资料，不打开支付页或创建订单"}
		}
		return map[string]any{"code": 0, "trade_no": trade, "pay_type": "qrcode", "pay_info": "DOCUMENTATION_SANDBOX_ONLY:" + trade}
	case "pay_query", "pay_refund", "pay_close":
		id := p["trade_no"]
		if id == "" {
			id = p["out_trade_no"]
		}
		order, ok := playgroundOrder(id)
		if !ok {
			return playgroundFailure("order not found：仅支持 DOC_PAID / DOC_PENDING / DOC_REFUNDED / DOC_FROZEN / DOC_AUTH 固定样例")
		}
		if spec.Slug == "pay_query" {
			return order
		}
		status := order["status"].(int)
		if spec.Slug == "pay_close" {
			if status != 0 {
				return playgroundFailure("仅未支付沙箱样例 DOC_PENDING 可演示关单")
			}
			return map[string]any{"code": 0, "trade_no": id, "msg": "模拟关闭资料；样例状态不会改变"}
		}
		if status != 1 && status != 3 {
			return playgroundFailure("退款需使用已付款样例 DOC_PAID 或 DOC_FROZEN")
		}
		if amount > 100 {
			return playgroundFailure("退款金额不能超过沙箱样例原支付金额 1.00")
		}
		refund := p["out_refund_no"]
		if refund == "" {
			refund = "DOC_REFUND_GENERATED"
		}
		return map[string]any{"code": 0, "refund_no": "21901", "out_refund_no": refund, "trade_no": id, "money": ledger.Format(amount), "reducemoney": ledger.Format(amount), "msg": "模拟退款受理；不执行退款，不改变样例"}
	case "pay_refundquery":
		id := p["out_refund_no"]
		if id == "" {
			switch p["refund_no"] {
			case "21901":
				id = "DOC_REFUND_001"
			case "21902":
				id = "DOC_REFUND_PENDING"
			case "21903":
				id = "DOC_REFUND_FAILED"
			}
		}
		switch id {
		case "DOC_REFUND_PENDING":
			return map[string]any{"code": -1, "msg": "refund processing", "refund_no": "21902", "out_refund_no": id}
		case "DOC_REFUND_001", "DOC_REFUND_FAILED":
			status := 1
			number := "21901"
			if id == "DOC_REFUND_FAILED" {
				status = 0
				number = "21903"
			}
			return map[string]any{"code": 0, "refund_no": number, "out_refund_no": id, "money": "0.50", "reducemoney": "0.50", "status": status}
		default:
			return playgroundFailure("refund not found：请选择文档中的固定退款样例")
		}
	case "merchant_info":
		return map[string]any{"code": 0, "pid": 900001, "status": 1, "pay_status": 1, "settle_status": 1, "money": "100.00", "settle_type": 0, "settle_account": "", "settle_name": "", "order_num": 5, "order_num_today": 3, "order_num_lastday": 2, "order_money_today": "2.00", "order_money_lastday": "1.00"}
	case "merchant_orders":
		offset, limit := int64(0), int64(20)
		var err error
		if p["offset"] != "" {
			offset, err = strconv.ParseInt(p["offset"], 10, 64)
			if err != nil || offset < 0 || offset > 10000000 {
				return playgroundFailure("offset 必须为 0—10000000 的整数")
			}
		}
		if p["limit"] != "" {
			limit, err = strconv.ParseInt(p["limit"], 10, 64)
			if err != nil || limit <= 0 || limit > 1000000 {
				return playgroundFailure("limit 必须为 1—1000000 的整数")
			}
		}
		if limit > 50 {
			limit = 50
		}
		filter := -1
		if p["status"] != "" {
			filter, err = strconv.Atoi(p["status"])
			if err != nil || filter < 0 || filter > 4 || len(p["status"]) != 1 {
				return playgroundFailure("status 仅接受 0、1、2、3、4")
			}
		}
		all := []any{}
		for _, id := range []string{"DOC_PAID", "DOC_PENDING", "DOC_REFUNDED", "DOC_FROZEN", "DOC_AUTH"} {
			order, _ := playgroundOrder(id)
			if filter < 0 || order["status"] == filter {
				delete(order, "code")
				delete(order, "clientip")
				delete(order, "authorization_release_state")
				all = append(all, order)
			}
		}
		data := []any{}
		if offset < int64(len(all)) {
			end := offset + limit
			if end > int64(len(all)) {
				end = int64(len(all))
			}
			data = append(data, all[int(offset):int(end)]...)
		}
		return map[string]any{"code": 0, "data": data}
	case "transfer_submit":
		if p["type"] != "alipay" && p["type"] != "wxpay" && p["type"] != "bank" {
			return playgroundFailure("转账方式仅支持 alipay、wxpay、bank")
		}
		if len(p["account"]) > 128 || len(p["name"]) > 128 {
			return playgroundFailure("收款账号或名称最长 128 字节")
		}
		for _, field := range []string{"account", "name", "remark"} {
			if !utf8.ValidString(p[field]) || strings.ContainsAny(p[field], "\x00\r\n") {
				return playgroundFailure(field + " 不能包含 NUL、回车或换行")
			}
		}
		if p["type"] == "bank" && strings.TrimSpace(p["name"]) == "" {
			return playgroundFailure("银行卡沙箱示例需要收款人名称")
		}
		if amount > 10000 {
			return playgroundFailure("沙箱可用余额为 100.00，模拟转账金额超过上限")
		}
		if utf8.RuneCountInString(p["remark"]) > 32 {
			return playgroundFailure("remark 最多 32 个字符")
		}
		biz := p["out_biz_no"]
		if biz == "" {
			biz = "DOC_TRANSFER_GENERATED"
		}
		return map[string]any{"code": 0, "status": 0, "biz_no": biz, "out_biz_no": biz, "orderid": "", "paydate": "", "amount": ledger.Format(amount), "cost_money": ledger.Format(amount), "remark": p["remark"], "query_required": true}
	case "transfer_query":
		id := p["out_biz_no"]
		if id == "" {
			id = p["biz_no"]
		}
		if p["out_biz_no"] != "" && p["biz_no"] != "" && p["out_biz_no"] != p["biz_no"] {
			return playgroundFailure("转账单号不匹配")
		}
		status := 1
		switch id {
		case "DOC_TRANSFER_001":
		case "DOC_TRANSFER_PENDING":
			status = 0
		case "DOC_TRANSFER_FAILED":
			status = 2
		default:
			return playgroundFailure("transfer not found：请选择固定转账样例")
		}
		out := map[string]any{"code": 0, "status": status, "biz_no": id, "out_biz_no": id, "orderid": "DOC_PROVIDER_RECEIPT", "paydate": "2026-01-01 12:00:00", "amount": "1.00", "cost_money": "1.00", "remark": "文档沙箱演示"}
		if status == 0 {
			out["query_required"] = true
			out["orderid"] = ""
			out["paydate"] = ""
		}
		if status == 2 {
			out["errmsg"] = "transfer failed"
		}
		return out
	case "transfer_balance":
		return map[string]any{"code": 0, "available_money": "100.00", "ledger_available_money": "100.00", "today_blocked_money": "0.00", "settle_type": 0, "transfer_rate": "0.00"}
	}
	return playgroundFailure("不支持的沙箱接口")
}

func playgroundOrder(id string) (map[string]any, bool) {
	status := 0
	switch id {
	case "DOC_PENDING":
	case "DOC_PAID":
		status = 1
	case "DOC_REFUNDED":
		status = 2
	case "DOC_FROZEN":
		status = 3
	case "DOC_AUTH":
		status = 4
	default:
		return nil, false
	}
	out := map[string]any{"code": 0, "trade_no": id, "out_trade_no": id, "api_trade_no": "", "pid": 900001, "type": "alipay", "name": "文档沙箱订单", "money": "1.00", "status": status, "param": "", "buyer": "", "clientip": "192.0.2.10", "addtime": "2026-01-01 12:00:00", "endtime": ""}
	if status == 1 || status == 2 || status == 3 {
		out["endtime"] = "2026-01-01 12:01:00"
	}
	if status == 4 {
		out["authorization_release_state"] = "pending"
		out["api_trade_no"] = "DOC_AUTH_RECEIPT"
	}
	return out, true
}
