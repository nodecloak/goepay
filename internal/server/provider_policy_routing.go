package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"math/big"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

func payRollIndex(strategy string, channels []backoffice.PayRollChannel, cursor, draw int64) (int, error) {
	if len(channels) == 0 || cursor < 0 {
		return 0, payment.ErrInvalid
	}
	switch strategy {
	case "first":
		return 0, nil
	case "round_robin":
		return int(cursor % int64(len(channels))), nil
	case "weighted":
		var total int64
		for _, c := range channels {
			total += c.Weight
		}
		if draw < 0 || draw >= total {
			return 0, payment.ErrInvalid
		}
		for i, c := range channels {
			if draw < c.Weight {
				return i, nil
			}
			draw -= c.Weight
		}
	case "priority":
		max := int64(-1)
		for _, c := range channels {
			if c.Weight > max {
				max = c.Weight
			}
		}
		for i, c := range channels {
			if c.Weight == max {
				if draw == 0 {
					return i, nil
				}
				draw--
			}
		}
	}
	return 0, payment.ErrInvalid
}
func routeAttemptMethod(kind, method string) string {
	if method == "applet" {
		if kind == "wxpay" {
			return "jsapi"
		}
		return "mini"
	}
	return method
}
func cloneRouteParams(p compat.Params, id int64) compat.Params {
	out := compat.Params{}
	for k, v := range p {
		out[k] = v
	}
	out["channel_id"] = strconv.FormatInt(id, 10)
	return out
}

// Pure local preflight; no material or upstream request is submitted here.
type providerRouteMode uint8

const (
	routeAllProducts providerRouteMode = iota
	routeBrowserProducts
)

func (s *Server) routeCandidate(mid, id, amount int64, p compat.Params, mode providerRouteMode) (string, error) {
	_, method, e := s.providerRoute(mid, cloneRouteParams(p, id))
	if e != nil {
		return "", e
	}
	if mode == routeBrowserProducts {
		switch method {
		case "page", "wap", "native", "h5", "app":
		default:
			return "", provider.ErrUnsupported
		}
	}
	if method == "scan" {
		return method, s.ProviderFlow.ValidateScanRequest(id, provider.ScanRequest{TradeNo: "routing-preflight", Subject: p["name"], Amount: amount, ClientIP: p["clientip"], AuthCode: p["auth_code"]})
	}
	if method == "applet" {
		mini, metadata, underlying, ok := s.miniLaunchChannelReady(mid, id)
		if !ok {
			return "", payment.ErrInvalid
		}
		b := payment.MiniLaunchBinding{TradeNo: "routing-preflight", MerchantID: mid, ChannelID: id, Provider: p["type"], AppID: metadata.AppID, Method: underlying, Purpose: "mini-payment", Path: metadata.Path}
		_, e = mini.MiniLaunch(provider.MiniLaunchRequest{TradeNo: b.TradeNo, Amount: amount, CompleteURL: s.miniCompleteURL(b, strings.Repeat("a", 64))})
		return method, e
	}
	openID := p["sub_openid"]
	if openID == "" && p["oauth"] == "1" {
		// Validate only the local product/IP/amount prerequisites. The placeholder
		// never reaches material generation or any payment/OAuth request.
		openID = "routing-preflight-identity"
	}
	base := strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	request := provider.CreateRequest{TradeNo: "routing-preflight", Subject: p["name"], Method: method, Amount: amount, ClientIP: p["clientip"], OpenID: openID, NotifyURL: base + fmt.Sprintf("/callbacks/providers/%d/routing-preflight", id), ReturnURL: base + "/checkout/routing-preflight/return"}
	if method == "preauth" {
		request.NotifyURL = base + "/pay/preauthnotify/routing-preflight/"
	}
	if v, ok := s.ProviderFlow.Providers[id].(provider.CreateValidator); ok {
		if e = v.ValidateCreate(request); e != nil {
			return "", e
		}
	}
	return method, nil
}
func (s *Server) routeRequestHash(mid int64, p compat.Params, channel int64, method string, amount int64) []byte {
	mac := hmac.New(sha256.New, s.CheckoutKey)
	mac.Write([]byte("goepay/provider-route/v1\x00"))
	auth := ""
	if method == "scan" {
		auth = p["auth_code"]
	}
	appID := ""
	if identity, ok := s.ProviderFlow.Providers[channel].(provider.ApplicationIdentity); ok {
		appID = identity.ApplicationID()
	}
	_ = json.NewEncoder(mac).Encode([]any{mid, p["out_trade_no"], channel, appID, p["type"], method, amount, p["name"], p["notify_url"], p["return_url"], p["param"], p["sub_openid"], p["is_applet"] == "1", p["oauth"] == "1", auth})
	return mac.Sum(nil)
}
func routeLock(ctx context.Context, tx pgx.Tx, key string) error {
	_, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key)
	return e
}

type routeStored struct {
	Channel, Rule int64
	Method        string
	Hash          []byte
}
type routeStats struct{ DayCount, DayAmount, Recent int64 }

func routeLimitFits(l backoffice.PayRollLimit, stats routeStats, amount int64) bool {
	return (l.DailyCount == 0 || stats.DayCount < l.DailyCount) && (l.DailyAmount == 0 || amount <= l.DailyAmount && stats.DayAmount <= l.DailyAmount-amount) && (l.RateCount == 0 || stats.Recent < l.RateCount)
}
func routeStatsFor(ctx context.Context, tx pgx.Tx, rule, channel int64, l backoffice.PayRollLimit, now time.Time) (routeStats, error) {
	var out routeStats
	local := now.In(time.FixedZone("UTC+8", 8*3600))
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).UTC()
	since := now.Add(-time.Duration(l.RateSeconds) * time.Second)
	if l.DailyCount == 0 && l.DailyAmount == 0 && l.RateCount == 0 {
		return out, nil
	}
	if channel == 0 {
		e := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE created_at >= $2::timestamptz),COALESCE(sum(amount) FILTER(WHERE created_at >= $2::timestamptz),0),count(*) FILTER(WHERE created_at >= $3::timestamptz) FROM provider_route_assignments WHERE rule_id=$1 AND created_at>=LEAST($2::timestamptz,$3::timestamptz)`, rule, day, since).Scan(&out.DayCount, &out.DayAmount, &out.Recent)
		return out, e
	}
	// Include historical direct attempts without counting new routed orders twice.
	e := tx.QueryRow(ctx, `WITH events AS (
 SELECT amount,created_at FROM provider_route_assignments WHERE channel_id=$1 AND created_at>=LEAST($2::timestamptz,$3::timestamptz)
 UNION ALL SELECT o.amount,o.created_at FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.channel_id=$1 AND o.created_at>=LEAST($2::timestamptz,$3::timestamptz)
 AND NOT EXISTS(SELECT 1 FROM provider_route_assignments r WHERE r.merchant_id=o.merchant_id AND r.out_trade_no=o.out_trade_no)
) SELECT count(*) FILTER(WHERE created_at >= $2::timestamptz),COALESCE(sum(amount) FILTER(WHERE created_at >= $2::timestamptz),0),count(*) FILTER(WHERE created_at >= $3::timestamptz) FROM events`, channel, day, since).Scan(&out.DayCount, &out.DayAmount, &out.Recent)
	return out, e
}

// providerRoutedRequest is called only after authentication and local parameter
// checks. The caller supplies the parsed actual/signed IP in a parameter copy.
// Assignment and quotas commit before any provider write and never fail over.
func (s *Server) providerRoutedRequest(ctx context.Context, mid int64, p compat.Params) (int64, string, error) {
	return s.providerRoutedRequestMode(ctx, mid, p, routeAllProducts)
}

// Browser-only callers choose the policy at the Go callsite, never from a
// signed or unsigned request parameter. Unsupported lifecycle products are
// removed before selection and quota commit.
func (s *Server) providerRoutedBrowserRequest(ctx context.Context, mid int64, p compat.Params) (int64, string, error) {
	return s.providerRoutedRequestMode(ctx, mid, p, routeBrowserProducts)
}
func (s *Server) providerRoutedRequestMode(ctx context.Context, mid int64, p compat.Params, mode providerRouteMode) (int64, string, error) {
	if s.Pool == nil || s.Store == nil || s.ProviderFlow == nil || !s.ProviderFlow.AllowLive || len(s.CheckoutKey) < 32 || mid <= 0 {
		return 0, "", payment.ErrInvalid
	}
	amount, e := ledger.Parse(p["money"])
	if e != nil || p["out_trade_no"] == "" || len(p["out_trade_no"]) > 128 || len(p["name"]) > 256 || !validURL(p["notify_url"]) || !validURL(p["return_url"]) || net.ParseIP(p["clientip"]) == nil {
		return 0, "", payment.ErrInvalid
	}
	if p["type"] != "alipay" && p["type"] != "wxpay" {
		return 0, "", payment.ErrInvalid
	}
	explicit := int64(0)
	if p["channel_id"] != "" {
		explicit, e = strconv.ParseInt(p["channel_id"], 10, 64)
		if e != nil || explicit <= 0 || strconv.FormatInt(explicit, 10) != p["channel_id"] {
			return 0, "", payment.ErrInvalid
		}
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return 0, "", e
	}
	defer tx.Rollback(ctx)
	if e = routeLock(ctx, tx, fmt.Sprintf("goepay/route/order/%d/%s", mid, p["out_trade_no"])); e != nil {
		return 0, "", e
	}
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", mid).Scan(&enabled); e != nil || !enabled {
		return 0, "", payment.ErrState
	}
	var existingOrderID int64
	e = tx.QueryRow(ctx, "SELECT id FROM orders WHERE merchant_id=$1 AND out_trade_no=$2 FOR UPDATE", mid, p["out_trade_no"]).Scan(&existingOrderID)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return 0, "", e
	}
	var attemptChannel int64
	attemptMethod := ""
	if existingOrderID > 0 {
		var name, kind, notify, ret, param string
		var oldAmount int64
		if e = tx.QueryRow(ctx, "SELECT name,type,amount,notify_url,return_url,param FROM orders WHERE id=$1", existingOrderID).Scan(&name, &kind, &oldAmount, &notify, &ret, &param); e != nil {
			return 0, "", e
		}
		if name != p["name"] || kind != p["type"] || oldAmount != amount || notify != p["notify_url"] || ret != p["return_url"] || param != p["param"] {
			return 0, "", payment.ErrConflict
		}
		e = tx.QueryRow(ctx, "SELECT channel_id,method FROM provider_attempts WHERE order_id=$1", existingOrderID).Scan(&attemptChannel, &attemptMethod)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return 0, "", e
		}
	}
	var stored routeStored
	e = tx.QueryRow(ctx, "SELECT channel_id,method,request_hash,COALESCE(rule_id,0) FROM provider_route_assignments WHERE merchant_id=$1 AND out_trade_no=$2", mid, p["out_trade_no"]).Scan(&stored.Channel, &stored.Method, &stored.Hash, &stored.Rule)
	if e == nil {
		if explicit != 0 && explicit != stored.Channel || attemptChannel != 0 && (attemptChannel != stored.Channel || attemptMethod != routeAttemptMethod(p["type"], stored.Method)) {
			return 0, "", payment.ErrConflict
		}
		method, e := s.routeCandidate(mid, stored.Channel, amount, p, mode)
		if e != nil || method != stored.Method || !hmac.Equal(s.routeRequestHash(mid, p, stored.Channel, method, amount), stored.Hash) {
			return 0, "", payment.ErrConflict
		}
		if stored.Rule > 0 && attemptChannel == 0 {
			var raw []byte
			e = tx.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE id=$1 AND resource='pay-rolls' FOR SHARE", stored.Rule).Scan(&raw)
			if e != nil {
				return 0, "", payment.ErrState
			}
			var fields map[string]any
			if json.Unmarshal(raw, &fields) != nil {
				return 0, "", payment.ErrState
			}
			rule, e := backoffice.ParsePayRoll(fields)
			member := false
			for _, candidate := range rule.Channels {
				if candidate.ID == stored.Channel {
					member = true
				}
			}
			if e != nil || !rule.Enabled || rule.Type != p["type"] || rule.MerchantID != 0 && rule.MerchantID != mid || !member {
				return 0, "", payment.ErrState
			}
		}
		if e = tx.Commit(ctx); e != nil {
			return 0, "", e
		}
		return stored.Channel, method, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return 0, "", e
	}
	// An old attempt is authoritative, regardless of newly configured roll order.
	if attemptChannel != 0 {
		if explicit != 0 && explicit != attemptChannel {
			return 0, "", payment.ErrConflict
		}
		method, e := s.routeCandidate(mid, attemptChannel, amount, p, mode)
		if e != nil || routeAttemptMethod(p["type"], method) != attemptMethod {
			return 0, "", payment.ErrConflict
		}
		// Its original attempt/identity is already durable. Do not backfill a
		// new identity from a retry that has not yet passed the 24 identity check.
		if e = tx.Commit(ctx); e != nil {
			return 0, "", e
		}
		return attemptChannel, method, nil
	}
	// Legacy prepared OAuth/mini contexts also retain their trusted channel.
	if existingOrderID > 0 {
		var intentChannel int64
		var intentCount int
		var intentMethod string
		e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(channel_id),0),COALESCE(min(method),'') FROM (SELECT channel_id,method FROM oauth_payment_intents WHERE order_id=$1 UNION ALL SELECT channel_id,'applet'::text FROM mini_payment_intents WHERE order_id=$1) i`, existingOrderID).Scan(&intentCount, &intentChannel, &intentMethod)
		if e != nil {
			return 0, "", e
		}
		if intentCount > 1 {
			return 0, "", payment.ErrConflict
		}
		if intentCount == 1 {
			if explicit != 0 && explicit != intentChannel {
				return 0, "", payment.ErrConflict
			}
			method, e := s.routeCandidate(mid, intentChannel, amount, p, mode)
			if e != nil || method != intentMethod {
				return 0, "", payment.ErrConflict
			}
			if e = tx.Commit(ctx); e != nil {
				return 0, "", e
			}
			return intentChannel, method, nil
		}
	}
	var ruleID int64
	var ruleBody []byte
	var rule backoffice.PayRoll
	rows, e := tx.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='pay-rolls' AND body ? 'merchant_id' AND body ? 'type' AND body->'enabled'='true'::jsonb AND body->>'type'=$1 AND body->>'merchant_id' IN ($2,'0') ORDER BY (body->>'merchant_id'=$2) DESC,id LIMIT 2 FOR SHARE`, p["type"], strconv.FormatInt(mid, 10))
	if e != nil {
		return 0, "", e
	}
	for rows.Next() {
		var id int64
		var body []byte
		if e = rows.Scan(&id, &body); e != nil {
			rows.Close()
			return 0, "", e
		}
		var v map[string]any
		if json.Unmarshal(body, &v) != nil {
			rows.Close()
			return 0, "", payment.ErrInvalid
		}
		parsed, e := backoffice.ParsePayRoll(v)
		if e != nil {
			rows.Close()
			return 0, "", e
		}
		if ruleID == 0 {
			ruleID = id
			ruleBody = body
			rule = parsed
		} else if rule.MerchantID == parsed.MerchantID {
			rows.Close()
			return 0, "", payment.ErrConflict
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return 0, "", e
	}
	channels := []backoffice.PayRollChannel{}
	if ruleID != 0 {
		channels = rule.Channels
		if e = routeLock(ctx, tx, fmt.Sprintf("goepay/route/rule/%d", ruleID)); e != nil {
			return 0, "", e
		}
	} else {
		for _, id := range s.MerchantChannels[mid] {
			channels = append(channels, backoffice.PayRollChannel{ID: id, Weight: 1})
		}
	}
	// Sort locks independently of display/selection order to avoid group deadlocks.
	ids := []int64{}
	for _, c := range channels {
		ids = append(ids, c.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if e = routeLock(ctx, tx, fmt.Sprintf("goepay/route/channel/%d", id)); e != nil {
			return 0, "", e
		}
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return 0, "", e
	}
	if ruleID != 0 {
		if !rule.Limits.Allows(amount, now) {
			return 0, "", payment.ErrState
		}
		stats, e := routeStatsFor(ctx, tx, ruleID, 0, rule.Limits, now)
		if e != nil {
			return 0, "", e
		}
		if !routeLimitFits(rule.Limits, stats, amount) {
			return 0, "", payment.ErrState
		}
	}
	eligible := []backoffice.PayRollChannel{}
	methods := map[int64]string{}
	for _, c := range channels {
		if explicit != 0 && explicit != c.ID {
			continue
		}
		method, e := s.routeCandidate(mid, c.ID, amount, p, mode)
		if e != nil {
			continue
		}
		if ruleID != 0 {
			l := rule.Rules[c.ID]
			if !l.Allows(amount, now) {
				continue
			}
			stats, e := routeStatsFor(ctx, tx, 0, c.ID, l, now)
			if e != nil {
				return 0, "", e
			}
			if !routeLimitFits(l, stats, amount) {
				continue
			}
		}
		eligible = append(eligible, c)
		methods[c.ID] = method
	}
	if len(eligible) == 0 {
		return 0, "", payment.ErrState
	}
	strategy := "first"
	cursor, draw := int64(0), int64(0)
	if ruleID != 0 {
		strategy = rule.Strategy
		if _, e = tx.Exec(ctx, "INSERT INTO provider_route_counters(rule_id) VALUES($1) ON CONFLICT DO NOTHING", ruleID); e != nil {
			return 0, "", e
		}
		if e = tx.QueryRow(ctx, "SELECT cursor FROM provider_route_counters WHERE rule_id=$1 FOR UPDATE", ruleID).Scan(&cursor); e != nil {
			return 0, "", e
		}
		var bound int64
		if strategy == "weighted" {
			for _, c := range eligible {
				bound += c.Weight
			}
		} else if strategy == "priority" {
			max := int64(-1)
			for _, c := range eligible {
				if c.Weight > max {
					max = c.Weight
					bound = 1
				} else if c.Weight == max {
					bound++
				}
			}
		}
		if strategy == "weighted" || strategy == "priority" {
			if bound <= 0 {
				return 0, "", payment.ErrState
			}
			n, e := rand.Int(rand.Reader, big.NewInt(bound))
			if e != nil {
				return 0, "", e
			}
			draw = n.Int64()
		}
	}
	index, e := payRollIndex(strategy, eligible, cursor, draw)
	if e != nil {
		return 0, "", e
	}
	chosen := eligible[index]
	method := methods[chosen.ID]
	if e = s.insertProviderRoute(ctx, tx, mid, p, chosen.ID, method, amount, ruleID, ruleBody); e != nil {
		return 0, "", e
	}
	if ruleID != 0 {
		next := cursor
		if strategy == "round_robin" {
			next = (cursor + 1) % int64(len(eligible))
		}
		if _, e = tx.Exec(ctx, "UPDATE provider_route_counters SET cursor=$2,assigned=assigned+1 WHERE rule_id=$1", ruleID, next); e != nil {
			return 0, "", e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, "", e
	}
	return chosen.ID, method, nil
}
func (s *Server) insertProviderRoute(ctx context.Context, tx pgx.Tx, mid int64, p compat.Params, id int64, method string, amount, ruleID int64, snapshot []byte) error {
	var rule any
	if ruleID != 0 {
		rule = ruleID
	}
	if len(snapshot) == 0 {
		snapshot = []byte("{}")
	}
	_, e := tx.Exec(ctx, `INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash,rule_id,rule_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, mid, p["out_trade_no"], id, method, p["type"], amount, s.routeRequestHash(mid, p, id, method, amount), rule, string(snapshot))
	return e
}
