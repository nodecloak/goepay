package backoffice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type OrderBlacklist struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	ExpiresAt string `json:"expires_at"`
	Enabled   bool   `json:"enabled"`
	Replayed  bool   `json:"replayed"`
}

var blacklistRequest = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func (s *Service) AddOrderBlacklist(ctx context.Context, p Principal, orderID int64, kind, expected string, days int, reason, external string) (OrderBlacklist, error) {
	var out OrderBlacklist
	if p.Role != "admin" || p.SubjectID < 1 || (p.AdminClass != "super" && p.AdminClass != "operator") {
		return out, ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "订单风险核对"
	}
	if orderID < 1 || (kind != "ip" && kind != "buyer") || days < 0 || days > 36500 || !blacklistRequest.MatchString(external) || !utf8.ValidString(reason) || len(reason) > 1000 || strings.ContainsAny(reason, "\x00\r\n") || expected == "" || len(expected) > 253 || strings.ContainsAny(expected, "\x00\r\n") {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var locked int64
	if e = tx.QueryRow(ctx, `SELECT id FROM orders WHERE id=$1 FOR SHARE`, orderID).Scan(&locked); e != nil {
		return out, businessError(e)
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/order-blacklist/%d/%d/%s", p.SubjectID, orderID, external)); e != nil {
		return out, e
	}
	raw, _ := json.Marshal([]any{orderID, kind, expected, days, reason, external})
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var prior []byte
	e = tx.QueryRow(ctx, `SELECT changes FROM backoffice_audit WHERE actor_role='admin' AND actor_id=$1 AND resource='order-blacklist' AND resource_id=$2 AND operation='create' AND changes->>'external_no'=$3 ORDER BY id LIMIT 1`, p.SubjectID, orderID, external).Scan(&prior)
	if e == nil {
		var saved struct {
			Hash string `json:"request_hash"`
			ID   string `json:"record_id"`
		}
		if json.Unmarshal(prior, &saved) != nil || saved.Hash != hash {
			return out, ErrConflict
		}
		var body []byte
		if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE id=$1::bigint AND resource='blacklist'`, saved.ID).Scan(&body); e != nil {
			return out, businessError(e)
		}
		if json.Unmarshal(body, &out) != nil {
			return out, ErrConflict
		}
		out.ID = saved.ID
		out.Replayed = true
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	var ip, buyer string
	if e = tx.QueryRow(ctx, `SELECT COALESCE(r.client_ip,''),COALESCE(NULLIF(r.buyer,''),a.buyer_id,'') FROM orders o LEFT JOIN order_reporting r ON r.order_id=o.id LEFT JOIN payment_authorizations a ON a.order_id=o.id WHERE o.id=$1`, orderID).Scan(&ip, &buyer); e != nil {
		return out, e
	}
	value := buyer
	if kind == "ip" {
		value = ip
		if net.ParseIP(value) == nil {
			return out, ErrInvalid
		}
	}
	if value == "" || value != expected {
		return out, ErrConflict
	}
	expiry := ""
	if days > 0 {
		expiry = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	}
	fields := map[string]any{"kind": kind, "value": value, "reason": reason, "enabled": true, "expires_at": expiry}
	raw, _ = json.Marshal(fields)
	var id int64
	if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('blacklist',$1) RETURNING id`, raw).Scan(&id); e != nil {
		return out, businessError(e)
	}
	if e = audit(ctx, tx, p, "order-blacklist", orderID, "create", map[string]any{"external_no": external, "request_hash": hash, "record_id": strconv.FormatInt(id, 10), "kind": kind, "value": value, "days": days, "expires_at": expiry}); e != nil {
		return out, e
	}
	out = OrderBlacklist{ID: strconv.FormatInt(id, 10), Kind: kind, Value: value, ExpiresAt: expiry, Enabled: true}
	return out, tx.Commit(ctx)
}

// Historical entries without an expiry remain permanent. A malformed stored
// expiry fails closed rather than silently weakening an enabled risk rule.
func blacklistExpired(v map[string]any, now time.Time) bool {
	expiry, ok := v["expires_at"].(string)
	if !ok || expiry == "" {
		return false
	}
	at, e := time.Parse(time.RFC3339Nano, expiry)
	return e == nil && !now.Before(at)
}
