package backoffice

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/collectionstyle"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// validateRelations executes under the metadata transaction. Rules share the
// source merchant lock so concurrent changes cannot exceed a 100% allocation.
func validateRelations(ctx context.Context, tx pgx.Tx, resource string, id, ownerID int64, v map[string]any) error {
	if resource == "collection-styles" {
		if asset := collectionstyle.AssetID(fmt.Sprint(v["url"])); asset != "" {
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM collection_style_assets WHERE id=$1)", asset).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				return ErrInvalid
			}
		}
	}
	if resource == "order-subject-policies" {
		return validateSubjectPolicyRelations(ctx, tx, v)
	}
	if resource == "profitsharing-rules" {
		var sourceID int64
		if e := tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR UPDATE", ownerID).Scan(&sourceID); e != nil {
			return businessError(e)
		}
		recipient, e := strconv.ParseInt(fmt.Sprint(v["recipient_id"]), 10, 64)
		if e != nil || recipient <= 0 || recipient == ownerID {
			return ErrInvalid
		}
		var enabled bool
		if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", recipient).Scan(&enabled); e != nil || !enabled {
			return ErrInvalid
		}
		if v["enabled"] == true {
			rate, e := strconv.ParseInt(fmt.Sprint(v["rate_bps"]), 10, 64)
			if e != nil || rate <= 0 || rate > 10000 {
				return ErrInvalid
			}
			var exceeded bool
			e = tx.QueryRow(ctx, "SELECT COALESCE(sum((body->>'rate_bps')::numeric),0)+$3::numeric>10000 FROM backoffice_records WHERE resource='profitsharing-rules' AND owner_id=$1 AND id<>$2 AND body->>'enabled'='true'", ownerID, id, rate).Scan(&exceeded)
			if e != nil {
				return e
			}
			if exceeded {
				return ErrInvalid
			}
		}
	}
	if resource == "complaints" {
		var amount int64
		if e := tx.QueryRow(ctx, "SELECT amount FROM orders WHERE trade_no=$1 AND merchant_id=$2", fmt.Sprint(v["trade_no"]), ownerID).Scan(&amount); e != nil {
			return ErrInvalid
		}
		if v["amount"] == nil {
			v["amount"] = strconv.FormatInt(amount, 10)
		} else {
			n, e := strconv.ParseInt(fmt.Sprint(v["amount"]), 10, 64)
			if e != nil || n > amount {
				return ErrInvalid
			}
		}
	}
	return nil
}
func validateGroup(ctx context.Context, tx pgx.Tx, profile map[string]any) error {
	raw, ok := profile["group_id"]
	if !ok || raw == "" {
		return nil
	}
	id, e := strconv.ParseInt(fmt.Sprint(raw), 10, 64)
	if e != nil || id < 0 {
		return ErrInvalid
	}
	var exists bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE id=$1 AND resource='groups')", id).Scan(&exists)
	if e != nil {
		return e
	}
	if !exists {
		return ErrInvalid
	}
	return nil
}

type PaymentContext struct {
	MerchantID                            int64
	ClientIP, Buyer, NotifyURL, ReturnURL string
}

// CheckPayment applies only persisted, explicitly enabled restrictions. Domain
// allowlisting is opt-in via the enforce_domains=true system setting.
func (s *Service) CheckPayment(ctx context.Context, p PaymentContext) error {
	if p.MerchantID <= 0 {
		return ErrInvalid
	}
	rows, e := s.pool.Query(ctx, "SELECT resource,body FROM backoffice_records WHERE (resource='blacklist' AND body->>'enabled'='true') OR (resource='merchant-risk' AND owner_id=$1 AND body->>'status' IN ('blocked','restricted')) OR (resource='settings' AND body->>'key'='enforce_domains')", p.MerchantID)
	if e != nil {
		return e
	}
	defer rows.Close()
	enforceDomains := false
	denied := ""
	for rows.Next() {
		var resource string
		var raw []byte
		if e = rows.Scan(&resource, &raw); e != nil {
			return e
		}
		v := decodeFields(raw)
		if resource == "settings" {
			enforceDomains = v["value"] == "true"
			continue
		}
		if resource == "merchant-risk" {
			denied = "merchant is restricted"
			continue
		}
		if reason := ActiveBlacklistReason(v, p, time.Now()); reason != "" {
			denied = reason
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	if denied == "" && enforceDomains {
		for _, rawURL := range []string{p.NotifyURL, p.ReturnURL} {
			if rawURL == "" {
				continue
			}
			u, e := url.Parse(rawURL)
			if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
				return ErrInvalid
			}
			var domains []string
			rs, e := s.pool.Query(ctx, "SELECT body->>'domain' FROM backoffice_records WHERE resource='domains' AND owner_id=$1 AND body->>'approved'='true' AND body->>'enabled'='true'", p.MerchantID)
			if e != nil {
				return e
			}
			for rs.Next() {
				var domain string
				if e = rs.Scan(&domain); e != nil {
					rs.Close()
					return e
				}
				domains = append(domains, domain)
			}
			e = rs.Err()
			rs.Close()
			if e != nil {
				return e
			}
			match := false
			for _, domain := range domains {
				if authorizationDomainMatches(u.Hostname(), domain) {
					match = true
					break
				}
			}
			if !match {
				denied = "callback domain is not authorized"
			}
		}
	}
	if denied != "" {
		s.recordRisk(ctx, p, denied)
		return ErrForbidden
	}
	return nil
}
func domainMatches(host, domain string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	domain = strings.TrimSuffix(strings.ToLower(domain), ".")
	return host == domain || strings.HasSuffix(host, "."+domain)
}
func (s *Service) recordRisk(ctx context.Context, p PaymentContext, reason string) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	var id int64
	e = tx.QueryRow(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('risk-records',jsonb_build_object('decision','deny','reason',$1::text,'source','payment-policy','merchant_id',$2::text)) RETURNING id", reason, strconv.FormatInt(p.MerchantID, 10)).Scan(&id)
	if e != nil {
		return
	}
	if e = audit(ctx, tx, Principal{Role: "system", Username: "payment-policy"}, "risk-records", id, "deny", map[string]any{"reason": reason, "merchant_id": strconv.FormatInt(p.MerchantID, 10)}); e != nil {
		return
	}
	tx.Commit(ctx)
}
