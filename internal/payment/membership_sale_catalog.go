package payment

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

type MembershipSaleOffer struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	PriceCents     string `json:"price_cents"`
	DurationMonths *int   `json:"duration_months"`
	DurationDays   int    `json:"duration_days"`
	Permanent      bool   `json:"permanent"`
	Sort           int64  `json:"sort"`
	Description    string `json:"description"`
}

func (s *Store) MembershipSalesCatalog(ctx context.Context, mid int64) ([]MembershipSaleOffer, bool, error) {
	out := []MembershipSaleOffer{}
	var global string
	if e := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='group_buy'),'true')`).Scan(&global); e != nil {
		return nil, false, e
	}
	if global != "true" {
		return out, false, nil
	}
	var at time.Time
	if e := s.pool.QueryRow(ctx, "SELECT pg_catalog.clock_timestamp()").Scan(&at); e != nil {
		return nil, false, e
	}
	group, _, enabled, e := membershipEffectiveGroup(ctx, s.pool, mid, at)
	if e != nil {
		return nil, false, e
	}
	if !enabled {
		return nil, false, ErrState
	}
	var renewableGroup int64
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE((SELECT group_id FROM memberships WHERE merchant_id=$1 AND expires_at>$2 AND expires_at<>$3),0)", mid, at, permanentMembershipExpiry).Scan(&renewableGroup); e != nil {
		return nil, false, e
	}
	rows, e := s.pool.Query(ctx, "SELECT id,body FROM backoffice_records WHERE resource='groups' ORDER BY id LIMIT 1001")
	if e != nil {
		return nil, false, e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 1000 {
			return nil, false, ErrInvalid
		}
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, false, e
		}
		if id == group && id != renewableGroup {
			continue
		}
		terms, e := parseMembershipSale(raw, 1, group)
		if e == ErrState {
			continue
		}
		if e != nil {
			return nil, false, e
		}
		var b map[string]json.RawMessage
		if json.Unmarshal(raw, &b) != nil {
			return nil, false, ErrInvalid
		}
		offer := MembershipSaleOffer{ID: strconv.FormatInt(id, 10), PriceCents: strconv.FormatInt(terms.UnitPrice, 10), Permanent: terms.Permanent, DurationDays: terms.LegacyDays}
		if terms.LegacyDays == 0 {
			months := terms.Months
			offer.DurationMonths = &months
		}
		json.Unmarshal(b["name"], &offer.Name)
		json.Unmarshal(b["sale_description"], &offer.Description)
		offer.Sort, _ = saleInteger(b["sale_sort"], 0)
		out = append(out, offer)
	}
	if e = rows.Err(); e != nil {
		return nil, false, e
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out, true, nil
}
