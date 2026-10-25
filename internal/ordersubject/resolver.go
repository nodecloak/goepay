package ordersubject

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
)

type Resolver struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Resolver { return &Resolver{pool: pool} }

// Resolve commits one title before the external send. Historical attempts that
// predate this table retain the original name: their upstream request is unknown.
func (r *Resolver) Resolve(ctx context.Context, o payment.Order, channelID int64) (string, error) {
	if r == nil || r.pool == nil || o.ID <= 0 || channelID <= 0 {
		return "", payment.ErrInvalid
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var name, trade, out string
	var mid, amount int64
	e = tx.QueryRow(ctx, "SELECT name,trade_no,out_trade_no,merchant_id,amount FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&name, &trade, &out, &mid, &amount)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", payment.ErrNotFound
	}
	if e != nil {
		return "", e
	}
	if trade != o.TradeNo || mid != o.MerchantID {
		return "", payment.ErrConflict
	}
	var subject string
	var captured int64
	e = tx.QueryRow(ctx, "SELECT subject,channel_id FROM provider_subject_snapshots WHERE order_id=$1", o.ID).Scan(&subject, &captured)
	if e == nil {
		if captured != channelID {
			return "", payment.ErrConflict
		}
		return subject, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return "", e
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=$1)", o.ID).Scan(&exists); e != nil {
		return "", e
	}
	if exists {
		return name, nil
	}
	rows, e := tx.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='order-subject-policies' AND body->'enabled'='true'::jsonb AND
 ((body->>'scope'='global' AND body->>'scope_id'='0') OR (body->>'scope'='merchant' AND body->>'scope_id'=$1) OR (body->>'scope'='channel' AND body->>'scope_id'=$2))
 ORDER BY CASE body->>'scope' WHEN 'global' THEN 0 WHEN 'merchant' THEN 1 ELSE 2 END,id FOR SHARE`, strconv.FormatInt(mid, 10), strconv.FormatInt(channelID, 10))
	if e != nil {
		return "", e
	}
	template, rulesText := "", ""
	snapshot := map[string]any{}
	var templateID, rulesID int64
	for rows.Next() {
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			break
		}
		var p struct {
			Scope    string `json:"scope"`
			ScopeID  string `json:"scope_id"`
			Template string `json:"template"`
			Rules    string `json:"rules"`
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			break
		}
		n, parse := strconv.ParseInt(p.ScopeID, 10, 64)
		policy := Policy{Scope: p.Scope, ScopeID: n, Template: p.Template, RulesText: p.Rules}
		if parse != nil || strconv.FormatInt(n, 10) != p.ScopeID || policy.Validate() != nil {
			e = payment.ErrInvalid
			break
		}
		if p.Template != "" {
			template = p.Template
			templateID = id
		}
		if p.Rules != "" {
			rulesText = p.Rules
			rulesID = id
		}
	}
	rows.Close()
	if e != nil {
		return "", e
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	snapshot["template_policy_id"] = templateID
	snapshot["rules_policy_id"] = rulesID
	snapshot["template"] = template
	snapshot["rules"] = rulesText
	configured := template != ""
	if rulesText != "" {
		rules, parse := ParseRules(rulesText)
		if parse != nil {
			return "", payment.ErrInvalid
		}
		for _, rule := range rules {
			if amount >= rule.Min && amount <= rule.Max {
				template = rule.Name
				configured = true
				break
			}
		}
	}
	subject = name
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return "", e
	}
	if configured {
		var profile []byte
		if e = tx.QueryRow(ctx, "SELECT profile FROM merchants WHERE id=$1", mid).Scan(&profile); e != nil {
			return "", e
		}
		var contacts map[string]any
		if e = json.Unmarshal(profile, &contacts); e != nil {
			return "", e
		}
		qq, _ := contacts["qq"].(string)
		phone, _ := contacts["phone"].(string)
		subject, e = Render(template, Variables{Name: name, TradeNo: trade, OutTradeNo: out, QQ: qq, Phone: phone, Now: now})
		if e != nil {
			return "", errors.Join(payment.ErrInvalid, e)
		}
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return "", e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO provider_subject_snapshots(order_id,channel_id,subject,configured,policy_snapshot,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6)", o.ID, channelID, subject, configured, string(raw), now); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return subject, nil
}
