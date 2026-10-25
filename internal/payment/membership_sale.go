package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// MembershipSaleTerms is an immutable purchase fact, authenticated with the
// existing private source proof. Old purchases without it use the old format.
type MembershipSaleTerms struct {
	Quantity          int    `json:"quantity"`
	UnitPrice         int64  `json:"unit_price_cents,string"`
	Months            int    `json:"months"`
	LegacyDays        int    `json:"legacy_days"`
	Permanent         bool   `json:"permanent"`
	ConfigurationHash string `json:"configuration_hash"`
}

func membershipSaleQuantity(values []int) (int, error) {
	if len(values) > 1 {
		return 0, ErrInvalid
	}
	n := 1
	if len(values) == 1 {
		n = values[0]
	}
	if n < 1 || n > 300 {
		return 0, ErrInvalid
	}
	return n, nil
}

func saleInteger(raw json.RawMessage, fallback int64) (int64, error) {
	if len(raw) == 0 {
		return fallback, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = string(raw)
	}
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil || strconv.FormatInt(n, 10) != value {
		return 0, ErrInvalid
	}
	return n, nil
}

func parseMembershipSale(raw []byte, quantity int, payerGroup int64) (MembershipSaleTerms, error) {
	terms := MembershipSaleTerms{Quantity: quantity}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return terms, ErrInvalid
	}
	var enabled bool
	if json.Unmarshal(body["enabled"], &enabled) != nil || !enabled {
		return terms, ErrState
	}
	if v, ok := body["sale_enabled"]; ok {
		if json.Unmarshal(v, &enabled) != nil {
			return terms, ErrInvalid
		}
		if !enabled {
			return terms, ErrState
		}
	}
	var visible string
	if v, ok := body["sale_visible"]; ok && json.Unmarshal(v, &visible) != nil {
		return terms, ErrInvalid
	}
	if visible != "" {
		found := false
		for _, part := range strings.Split(visible, ",") {
			n, e := strconv.ParseInt(part, 10, 64)
			if e != nil || n < 0 || strconv.FormatInt(n, 10) != part {
				return terms, ErrInvalid
			}
			found = found || n == payerGroup
		}
		if !found {
			return terms, ErrState
		}
	}
	var e error
	terms.UnitPrice, e = saleInteger(body["price_cents"], 0)
	if e != nil || terms.UnitPrice < 0 || terms.UnitPrice > 100000000000 {
		return terms, ErrInvalid
	}
	if _, ok := body["duration_months"]; ok {
		months, e := saleInteger(body["duration_months"], 0)
		if e != nil || months < 0 || months > 1200 {
			return terms, ErrInvalid
		}
		terms.Months = int(months)
		terms.Permanent = months == 0
	} else {
		days, e := saleInteger(body["duration_days"], 30)
		if e != nil || days < 1 || days > 3660 {
			return terms, ErrInvalid
		}
		terms.LegacyDays = int(days)
	}
	return terms, nil
}

func (s *Store) FundsMembershipSaleSnapshot(ctx context.Context, q referralReader, mid, gid int64, quantity int) (MembershipSaleTerms, error) {
	var zero MembershipSaleTerms
	if _, e := membershipSaleQuantity([]int{quantity}); e != nil || mid <= 0 || gid <= 0 {
		return zero, ErrInvalid
	}
	var global string
	e := q.QueryRow(ctx, `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='group_buy'),'true')`).Scan(&global)
	if e != nil {
		return zero, e
	}
	if global != "true" {
		return zero, ErrState
	}
	var at time.Time
	if e = q.QueryRow(ctx, "SELECT pg_catalog.clock_timestamp()").Scan(&at); e != nil {
		return zero, e
	}
	payerGroup, _, enabled, e := membershipEffectiveGroup(ctx, q, mid, at)
	if e != nil {
		return zero, e
	}
	if !enabled {
		return zero, ErrState
	}
	if payerGroup == gid {
		var renewable bool
		if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memberships WHERE merchant_id=$1 AND group_id=$2 AND expires_at>$3 AND expires_at<>$4)", mid, gid, at, permanentMembershipExpiry).Scan(&renewable); e != nil {
			return zero, e
		}
		if !renewable {
			return zero, ErrState
		}
	}
	var raw []byte
	if e = q.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1", gid).Scan(&raw); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrNotFound
		}
		return zero, e
	}
	terms, e := parseMembershipSale(raw, quantity, payerGroup)
	if e != nil {
		return zero, e
	}
	// JSONB supplies deterministic key ordering. Including the whole original
	// group invalidates authorization when price, visibility or period changes.
	digest := sha256.Sum256(append(append([]byte{}, raw...), []byte("/"+global)...))
	terms.ConfigurationHash = hex.EncodeToString(digest[:])
	return terms, nil
}

var membershipSaleZone = time.FixedZone("UTC+08", 8*3600)
var permanentMembershipExpiry = time.Date(9999, 12, 31, 0, 0, 0, 0, membershipSaleZone)

func membershipSaleExpiry(terms MembershipSaleTerms, base time.Time) (time.Time, int, error) {
	if _, e := membershipSaleQuantity([]int{terms.Quantity}); e != nil {
		return time.Time{}, 0, e
	}
	if terms.Permanent {
		return permanentMembershipExpiry, 0, nil
	}
	var expiry time.Time
	if terms.LegacyDays > 0 {
		expiry = base.AddDate(0, 0, terms.LegacyDays*terms.Quantity)
	} else {
		if terms.Months < 1 || terms.Months > 1200 {
			return time.Time{}, 0, ErrInvalid
		}
		d := base.In(membershipSaleZone).AddDate(0, terms.Months*terms.Quantity, 0)
		expiry = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, membershipSaleZone)
	}
	if expiry.Year() > 9999 || !expiry.After(base) {
		return time.Time{}, 0, ErrInvalid
	}
	days := int((expiry.Unix() - base.Unix() + 86399) / 86400)
	return expiry, days, nil
}
