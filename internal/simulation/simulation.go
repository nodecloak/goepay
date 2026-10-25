// Package simulation implements local-only payment exercises without any upstream network access.
package simulation

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrSimulation = errors.New("invalid local simulation request")

type Environment struct {
	key       []byte
	base      string
	mu        sync.Mutex
	orders    map[string]record
	refunds   map[string]simRefund
	transfers map[string]simTransfer
	Notify    func(context.Context, int64, string, []byte) error
}
type record struct {
	ChannelID int64  `json:"channel_id"`
	Trade     string `json:"trade_no"`
	Amount    int64  `json:"amount"`
	Expires   int64  `json:"expires"`
	Paid      bool   `json:"-"`
	Closed    bool   `json:"-"`
}
type simRefund struct {
	Trade  string
	Total  int64
	Result provider.RefundResult
}
type simTransfer struct {
	Request provider.TransferRequest
	Result  provider.TransferResult
}
type client struct {
	env  *Environment
	id   int64
	kind string
}

func New(seed []byte, base string) (*Environment, error) {
	u, e := url.Parse(base)
	if len(seed) < 32 || e != nil || u.Scheme != "http" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, ErrSimulation
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, ErrSimulation
	}
	h := sha256.Sum256(append([]byte("local-simulation:"), seed...))
	return &Environment{key: h[:], base: base, orders: map[string]record{}, refunds: map[string]simRefund{}, transfers: map[string]simTransfer{}}, nil
}
func (e *Environment) Providers() map[int64]provider.Provider {
	return map[int64]provider.Provider{1: &client{e, 1, "alipay"}, 2: &client{e, 2, "wxpay"}}
}
func (e *Environment) sign(b []byte) string {
	h := hmac.New(sha256.New, e.key)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (e *Environment) token(r record) string {
	b, _ := json.Marshal(r)
	return base64.RawURLEncoding.EncodeToString(b) + "." + e.sign(b)
}
func (e *Environment) parse(token string) (record, error) {
	var r record
	p := strings.Split(token, ".")
	if len(p) != 2 {
		return r, ErrSimulation
	}
	b, err := base64.RawURLEncoding.DecodeString(p[0])
	if err != nil || len(b) > 2048 || !hmac.Equal([]byte(p[1]), []byte(e.sign(b))) || json.Unmarshal(b, &r) != nil || r.Expires < time.Now().Unix() {
		return record{}, ErrSimulation
	}
	return r, nil
}
func (c *client) ID() string { return c.kind }

// A synthetic identity for local immutable-intent tests; never a real AppID.
func (c *client) ApplicationID() string           { return fmt.Sprintf("simulation-%s-%d", c.kind, c.id) }
func (c *client) TransferAccountIdentity() string { return c.ApplicationID() }
func (c *client) Capabilities() provider.Capabilities {
	if c.kind == "wxpay" {
		return provider.Capabilities{Methods: []string{"native", "jsapi"}, Query: true, Close: true, Refund: true, RefundQuery: true}
	}
	return provider.Capabilities{Methods: []string{"page", "wap"}, Query: true, Close: true, Refund: true, RefundQuery: true}
}
func (c *client) Create(_ context.Context, in provider.CreateRequest) (provider.CreateResult, error) {
	if in.Amount <= 0 || in.TradeNo == "" {
		return provider.CreateResult{}, ErrSimulation
	}
	valid := false
	for _, m := range c.Capabilities().Methods {
		valid = valid || m == in.Method
	}
	if !valid {
		return provider.CreateResult{}, provider.ErrUnsupported
	}
	key := fmt.Sprintf("%d:%s", c.id, in.TradeNo)
	c.env.mu.Lock()
	r, ok := c.env.orders[key]
	if !ok {
		r = record{ChannelID: c.id, Trade: in.TradeNo, Amount: in.Amount, Expires: time.Now().Add(time.Hour).Unix()}
		c.env.orders[key] = r
	}
	c.env.mu.Unlock()
	if r.Amount != in.Amount {
		return provider.CreateResult{}, ErrSimulation
	}
	address := c.env.base + "/simulation/pay?token=" + url.QueryEscape(c.env.token(r))
	kind := "jump"
	if in.Method == "native" {
		kind = "qrcode"
	}
	return provider.CreateResult{MaterialType: kind, Material: address, ProviderTradeNo: "sim-" + in.TradeNo}, nil
}
func (c *client) Query(_ context.Context, in provider.QueryRequest) (provider.QueryResult, error) {
	c.env.mu.Lock()
	r, ok := c.env.orders[fmt.Sprintf("%d:%s", c.id, in.TradeNo)]
	c.env.mu.Unlock()
	if !ok {
		return provider.QueryResult{}, ErrSimulation
	}
	state := "NOTPAY"
	if r.Closed {
		state = "CLOSED"
	}
	if r.Paid {
		state = "SUCCESS"
	}
	return provider.QueryResult{TradeNo: in.TradeNo, ProviderTradeNo: "sim-" + in.TradeNo, State: state, Currency: "CNY", Amount: r.Amount}, nil
}
func (c *client) Close(_ context.Context, in provider.QueryRequest) error {
	c.env.mu.Lock()
	defer c.env.mu.Unlock()
	key := fmt.Sprintf("%d:%s", c.id, in.TradeNo)
	r, ok := c.env.orders[key]
	if !ok {
		return ErrSimulation
	}
	if r.Paid {
		return &provider.ProviderError{Code: "ORDERPAID"}
	}
	r.Closed = true
	c.env.orders[key] = r
	return nil
}
func (c *client) Refund(_ context.Context, in provider.RefundRequest) (provider.RefundResult, error) {
	c.env.mu.Lock()
	defer c.env.mu.Unlock()
	if in.RefundNo == "" || in.Amount <= 0 {
		return provider.RefundResult{}, ErrSimulation
	}
	key := fmt.Sprintf("%d:%s", c.id, in.RefundNo)
	if old, ok := c.env.refunds[key]; ok {
		if old.Trade != in.TradeNo || old.Total != in.TotalAmount || old.Result.Amount != in.Amount {
			return provider.RefundResult{}, ErrSimulation
		}
		return old.Result, nil
	}
	r, ok := c.env.orders[fmt.Sprintf("%d:%s", c.id, in.TradeNo)]
	if !ok || !r.Paid || r.Amount != in.TotalAmount {
		return provider.RefundResult{}, ErrSimulation
	}
	used := int64(0)
	for _, old := range c.env.refunds {
		if old.Trade == in.TradeNo {
			used += old.Result.Amount
		}
	}
	if used > r.Amount || in.Amount > r.Amount-used {
		return provider.RefundResult{}, ErrSimulation
	}
	result := provider.RefundResult{RefundNo: in.RefundNo, ProviderRefundNo: "sim-" + in.RefundNo, State: "SUCCESS", Amount: in.Amount}
	c.env.refunds[key] = simRefund{in.TradeNo, in.TotalAmount, result}
	return result, nil
}
func (c *client) QueryRefund(_ context.Context, in provider.RefundRequest) (provider.RefundResult, error) {
	c.env.mu.Lock()
	defer c.env.mu.Unlock()
	old, ok := c.env.refunds[fmt.Sprintf("%d:%s", c.id, in.RefundNo)]
	if !ok || old.Trade != in.TradeNo || old.Total != in.TotalAmount || old.Result.Amount != in.Amount {
		return provider.RefundResult{}, ErrSimulation
	}
	return old.Result, nil
}
func (c *client) VerifyNotification(_ context.Context, _ http.Header, raw []byte, expected provider.ExpectedPayment) (provider.PaymentEvent, error) {
	var envelope struct {
		Record record `json:"record"`
		MAC    string `json:"mac"`
	}
	if len(raw) > 4096 || json.Unmarshal(raw, &envelope) != nil {
		return provider.PaymentEvent{}, ErrSimulation
	}
	b, _ := json.Marshal(envelope.Record)
	r := envelope.Record
	if !hmac.Equal([]byte(envelope.MAC), []byte(c.env.sign(b))) || r.ChannelID != c.id || r.Trade != expected.TradeNo || r.Amount != expected.Amount {
		return provider.PaymentEvent{}, ErrSimulation
	}
	return provider.PaymentEvent{EventID: "sim-" + r.Trade, TradeNo: r.Trade, ProviderTradeNo: "sim-" + r.Trade, Amount: r.Amount, Currency: "CNY", State: "SUCCESS"}, nil
}
func (e *Environment) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path != "/simulation/pay" || (r.Method != "GET" && r.Method != "POST") {
		http.NotFound(w, r)
		return
	}
	record, err := e.parse(r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "invalid simulation token", 403)
		return
	}
	if r.Method == "POST" {
		e.mu.Lock()
		saved := e.orders[fmt.Sprintf("%d:%s", record.ChannelID, record.Trade)]
		closed := saved.Closed
		e.mu.Unlock()
		if closed {
			http.Error(w, "simulation order closed", 409)
			return
		}
		if e.Notify == nil {
			http.Error(w, "simulation callback unavailable", 503)
			return
		}
		b, _ := json.Marshal(record)
		raw, _ := json.Marshal(map[string]any{"record": record, "mac": e.sign(b)})
		if err = e.Notify(r.Context(), record.ChannelID, record.Trade, raw); err != nil {
			http.Error(w, "simulation callback rejected", 409)
			return
		}
		e.mu.Lock()
		saved, exists := e.orders[fmt.Sprintf("%d:%s", record.ChannelID, record.Trade)]
		if !exists {
			saved = record
		}
		saved.Paid = true
		e.orders[fmt.Sprintf("%d:%s", record.ChannelID, record.Trade)] = saved
		e.mu.Unlock()
		fmt.Fprint(w, "<!doctype html><html lang=zh-CN><meta charset=utf-8><title>模拟支付结果</title><h1>模拟支付已入账</h1><p>仅本地测试，无真实扣款。</p></html>")
		return
	}
	money := strconv.FormatInt(record.Amount/100, 10) + fmt.Sprintf(".%02d", record.Amount%100)
	fmt.Fprintf(w, "<!doctype html><html lang=zh-CN><meta charset=utf-8><title>本地模拟支付</title><h1>本地模拟支付</h1><p>订单 %s</p><p>测试金额 %s 元</p><p>仅本地账本演练，不涉及真实资金。</p><form method=post action=\"%s\"><button type=submit>确认模拟支付</button></form></html>", html.EscapeString(record.Trade), money, html.EscapeString(r.URL.RequestURI()))
}
