package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type membershipSaleGroupInput struct {
	ID             string `json:"id"`
	SaleEnabled    bool   `json:"sale_enabled"`
	PriceCents     string `json:"price_cents"`
	DurationMonths *int   `json:"duration_months"`
	Sort           int    `json:"sort"`
	Visible        string `json:"visible"`
	Description    string `json:"description"`
}
type membershipSaleGroup struct {
	membershipSaleGroupInput
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	DurationDays int    `json:"duration_days"`
}
type membershipSaleConfiguration struct {
	Enabled  bool                  `json:"enabled"`
	Revision string                `json:"revision"`
	Groups   []membershipSaleGroup `json:"groups"`
}
type membershipSaleQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readMembershipSaleConfiguration(ctx context.Context, q membershipSaleQuery, lock bool) (membershipSaleConfiguration, map[int64]map[string]json.RawMessage, error) {
	out := membershipSaleConfiguration{Groups: []membershipSaleGroup{}}
	var global string
	if e := q.QueryRow(ctx, `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='group_buy'),'true')`).Scan(&global); e != nil {
		return out, nil, e
	}
	if global != "true" && global != "false" {
		return out, nil, payment.ErrInvalid
	}
	out.Enabled = global == "true"
	query := "SELECT id,body FROM backoffice_records WHERE resource='groups' ORDER BY id LIMIT 1001"
	if lock {
		query += " FOR UPDATE"
	}
	rows, e := q.Query(ctx, query)
	if e != nil {
		return out, nil, e
	}
	defer rows.Close()
	bodies := map[int64]map[string]json.RawMessage{}
	facts := []any{global}
	for rows.Next() {
		if len(out.Groups) >= 1000 {
			return out, nil, payment.ErrInvalid
		}
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			return out, nil, e
		}
		b := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &b) != nil {
			return out, nil, payment.ErrInvalid
		}
		bodies[id] = b
		g := membershipSaleGroup{membershipSaleGroupInput: membershipSaleGroupInput{ID: strconv.FormatInt(id, 10), SaleEnabled: true, PriceCents: "0"}, DurationDays: 30}
		json.Unmarshal(b["name"], &g.Name)
		json.Unmarshal(b["enabled"], &g.Enabled)
		if v, ok := b["sale_enabled"]; ok && json.Unmarshal(v, &g.SaleEnabled) != nil {
			return out, nil, payment.ErrInvalid
		}
		readInt := func(key string, def int64) (int64, error) {
			v, ok := b[key]
			if !ok {
				return def, nil
			}
			var text string
			if json.Unmarshal(v, &text) != nil {
				text = string(v)
			}
			n, e := strconv.ParseInt(text, 10, 64)
			if e != nil || strconv.FormatInt(n, 10) != text {
				return 0, payment.ErrInvalid
			}
			return n, nil
		}
		price, e := readInt("price_cents", 0)
		if e != nil || price < 0 || price > 100000000000 {
			return out, nil, payment.ErrInvalid
		}
		g.PriceCents = strconv.FormatInt(price, 10)
		days, e := readInt("duration_days", 30)
		if e != nil {
			return out, nil, e
		}
		g.DurationDays = int(days)
		if _, ok := b["duration_months"]; ok {
			n, e := readInt("duration_months", 0)
			if e != nil || n < 0 || n > 1200 {
				return out, nil, payment.ErrInvalid
			}
			m := int(n)
			g.DurationMonths = &m
		}
		sort, e := readInt("sale_sort", 0)
		if e != nil {
			return out, nil, e
		}
		g.Sort = int(sort)
		json.Unmarshal(b["sale_visible"], &g.Visible)
		json.Unmarshal(b["sale_description"], &g.Description)
		out.Groups = append(out.Groups, g)
		facts = append(facts, id, json.RawMessage(raw))
	}
	if e = rows.Err(); e != nil {
		return out, nil, e
	}
	raw, e := json.Marshal(facts)
	if e != nil {
		return out, nil, e
	}
	hash := sha256.Sum256(raw)
	out.Revision = hex.EncodeToString(hash[:])
	return out, bodies, nil
}

func (s *Server) membershipSalesConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "membership-sales" {
		return false
	}
	if len(parts) != 2 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	if role == "merchant" {
		if r.Method != "GET" {
			response(w, 403, -1, "administrator configuration required", nil)
			return true
		}
		offers, enabled, e := s.Store.MembershipSalesCatalog(r.Context(), session.User.ID)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "", map[string]any{"items": offers, "enabled": enabled})
		}
		return true
	}
	if r.Method == "GET" {
		configuration, _, e := readMembershipSaleConfiguration(r.Context(), s.Pool, false)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "", configuration)
		}
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "GET or POST required", nil)
		return true
	}
	if session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "own super administrator session required", nil)
		return true
	}
	var input struct {
		Enabled          *bool                      `json:"enabled"`
		ExpectedRevision string                     `json:"expected_revision"`
		Groups           []membershipSaleGroupInput `json:"groups"`
	}
	if !decode(w, r, &input) {
		return true
	}
	if input.Enabled == nil || len(input.ExpectedRevision) != 64 || len(input.Groups) > 1000 {
		response(w, 422, -1, "configuration revision and enabled flag required", nil)
		return true
	}
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		s.fail(w, e)
		return true
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended('goepay/membership-sales',0))"); e != nil {
		s.fail(w, e)
		return true
	}
	current, bodies, e := readMembershipSaleConfiguration(r.Context(), tx, true)
	if e != nil {
		s.fail(w, e)
		return true
	}
	if current.Revision != input.ExpectedRevision {
		response(w, 409, -1, "configuration changed; reload before saving", nil)
		return true
	}
	seen := map[int64]bool{}
	for _, g := range input.Groups {
		id, e := strconv.ParseInt(g.ID, 10, 64)
		price, pe := strconv.ParseInt(g.PriceCents, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != g.ID || bodies[id] == nil || seen[id] || pe != nil || price < 0 || price > 100000000000 || strconv.FormatInt(price, 10) != g.PriceCents || g.DurationMonths != nil && (*g.DurationMonths < 0 || *g.DurationMonths > 1200) || g.Sort < 0 || g.Sort > 1000000 || !utf8.ValidString(g.Description) || utf8.RuneCountInString(g.Description) > 2000 || strings.ContainsRune(g.Description, 0) {
			response(w, 422, -1, "invalid group sales configuration", nil)
			return true
		}
		seen[id] = true
		if g.Visible != "" {
			for _, part := range strings.Split(g.Visible, ",") {
				n, e := strconv.ParseInt(part, 10, 64)
				if e != nil || n < 0 || strconv.FormatInt(n, 10) != part || n > 0 && bodies[n] == nil {
					response(w, 422, -1, "visible groups must be existing IDs separated by commas; 0 is the default group", nil)
					return true
				}
			}
		}
		body := bodies[id]
		values := map[string]any{"sale_enabled": g.SaleEnabled, "price_cents": g.PriceCents, "sale_sort": g.Sort, "sale_visible": g.Visible, "sale_description": g.Description}
		if g.DurationMonths == nil {
			delete(body, "duration_months")
		} else {
			values["duration_months"] = *g.DurationMonths
		}
		for k, v := range values {
			body[k], _ = json.Marshal(v)
		}
		raw, _ := json.Marshal(body)
		if _, e = tx.Exec(r.Context(), "UPDATE backoffice_records SET body=$2,updated_at=now() WHERE id=$1 AND resource='groups'", id, raw); e != nil {
			s.fail(w, e)
			return true
		}
	}
	raw, _ := json.Marshal(map[string]any{"key": "group_buy", "value": strconv.FormatBool(*input.Enabled)})
	var setting int64
	e = tx.QueryRow(r.Context(), "SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='group_buy' FOR UPDATE").Scan(&setting)
	if errors.Is(e, pgx.ErrNoRows) {
		_, e = tx.Exec(r.Context(), "INSERT INTO backoffice_records(resource,body) VALUES('settings',$1)", raw)
	} else if e == nil {
		_, e = tx.Exec(r.Context(), "UPDATE backoffice_records SET body=$2,updated_at=now() WHERE id=$1", setting, raw)
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	audit, _ := json.Marshal(map[string]any{"enabled": *input.Enabled, "groups": input.Groups, "previous_revision": current.Revision})
	if _, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'membership-sales',0,'configure',$3)", session.User.ID, session.User.Username, audit); e != nil {
		s.fail(w, e)
		return true
	}
	next, _, e := readMembershipSaleConfiguration(r.Context(), tx, false)
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		s.fail(w, e)
	} else {
		response(w, 200, 0, fmt.Sprintf("saved %d group sales configurations", len(input.Groups)), next)
	}
	return true
}
