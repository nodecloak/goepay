package backoffice

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/collectionstyle"
	"goepay/internal/merchantpolicy"
	"goepay/internal/payment"
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
		// N3 multi-receiver: a rule may carry `receivers: [{recipient_id, rate_bps}]`
		// (1..10). A legacy single `recipient_id`+`rate_bps` body is normalized to a
		// one-element list. The top-level `rate_bps` always mirrors the sum of the
		// receivers' rates, so the cross-rule Σ<=100% guard below (and every existing
		// reader of body->>'rate_bps') keeps working unchanged.
		receivers, e := normalizeInternalReceivers(ownerID, v)
		if e != nil {
			return e
		}
		for _, rcv := range receivers {
			var enabled bool
			if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", rcv.recipient).Scan(&enabled); e != nil || !enabled {
				return ErrInvalid
			}
		}
		if v["enabled"] == true {
			var total int64
			for _, rcv := range receivers {
				total += rcv.rate
			}
			if total <= 0 || total > 10000 {
				return ErrInvalid
			}
			var exceeded bool
			e = tx.QueryRow(ctx, "SELECT COALESCE(sum((body->>'rate_bps')::numeric),0)+$3::numeric>10000 FROM backoffice_records WHERE resource='profitsharing-rules' AND owner_id=$1 AND id<>$2 AND body->>'enabled'='true'", ownerID, id, total).Scan(&exceeded)
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

// MerchantContactQQ returns the merchant's stored contact QQ (merchants.profile
// ->>'qq'), "" when unset. Used by the create gate's forceqq check.
func (s *Service) MerchantContactQQ(ctx context.Context, merchantID int64) (string, error) {
	var qq *string
	e := s.pool.QueryRow(ctx, `SELECT CASE WHEN jsonb_typeof(profile->'qq')='string' THEN profile->>'qq' END FROM merchants WHERE id=$1`, merchantID).Scan(&qq)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return "", nil
		}
		return "", e
	}
	if qq == nil {
		return "", nil
	}
	return *qq, nil
}

type PaymentContext struct {
	MerchantID                            int64
	ClientIP, Buyer, NotifyURL, ReturnURL string
	// Name is the order goods name (for the blockname keyword gate); MerchantQQ
	// is the merchant's contact QQ (for the forceqq gate). Both optional: a
	// caller that leaves them empty keeps today's behaviour (blockname with an
	// empty name never matches a non-empty keyword; forceqq needs forceqq=1 AND
	// an empty QQ, and an unset QQ only blocks when forceqq is actually on).
	Name, MerchantQQ string
	// Internal authenticated evidence only. Domain admission is a new-intent
	// rule; all immutable order fields must match before it can be skipped.
	OrderIntent *payment.CreateOrderInput
}

// PaymentRefusedError carries the original merchant-facing refusal message
// (blackmsg on a blacklist hit, the forceqq message, or the blockalert text)
// while still unwrapping to ErrForbidden, so existing callers that test
// errors.Is(err, ErrForbidden) are unaffected and the create path can show the
// configured message.
type PaymentRefusedError struct{ Message string }

func (e PaymentRefusedError) Error() string { return e.Message }
func (e PaymentRefusedError) Unwrap() error { return ErrForbidden }

// CheckPayment applies only persisted, explicitly enabled restrictions. Domain
// allowlisting uses the effective group's pay_domain_forbid over the global
// enforce_domains setting, only for new payment intents. The
// 商品屏蔽(blockname/blockalert), 未填QQ禁止收款(forceqq) and黑名单提示(blackmsg)
// settings are applied here too, matching the original Pay.php create gate
// (Pay.php:81 forceqq, :103 blockname, :124 blackmsg). Each defaults off/empty
// so today's behaviour is unchanged when nothing is configured.
func (s *Service) CheckPayment(ctx context.Context, p PaymentContext) error {
	if p.MerchantID <= 0 {
		return ErrInvalid
	}
	rows, e := s.pool.Query(ctx, "SELECT resource,body FROM backoffice_records WHERE (resource='blacklist' AND body->>'enabled'='true') OR (resource='merchant-risk' AND owner_id=$1 AND body->>'status' IN ('blocked','restricted')) OR (resource='settings' AND body->>'key' IN ('blackmsg','blockname','blockalert','forceqq'))", p.MerchantID)
	if e != nil {
		return e
	}
	defer rows.Close()
	denied := ""
	message := ""
	blacklisted := false
	settings := map[string]string{}
	for rows.Next() {
		var resource string
		var raw []byte
		if e = rows.Scan(&resource, &raw); e != nil {
			return e
		}
		v := decodeFields(raw)
		if resource == "settings" {
			key := fmt.Sprint(v["key"])
			settings[key] = fmt.Sprint(v["value"])
			continue
		}
		if resource == "merchant-risk" {
			denied = "merchant is restricted"
			continue
		}
		if reason := ActiveBlacklistReason(v, p, time.Now()); reason != "" {
			denied = reason
			blacklisted = true
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	// A blacklisted payer sees the configured blackmsg (or its default). Resolved
	// after the full scan because the blackmsg row may load after the blacklist
	// row (Pay.php:124). Other denials keep the generic ErrForbidden message.
	if blacklisted {
		message = BlacklistMessage(settings["blackmsg"])
	}
	// forceqq: a merchant with no contact QQ cannot collect (Pay.php:81).
	if denied == "" && ForceQQBlocksPayment(settings["forceqq"], p.MerchantQQ) {
		denied = "merchant contact QQ required"
		message = "当前商户未填写联系QQ，无法收款"
	}
	// blockname: a goods name containing a listed keyword is refused with the
	// blockalert text, and the hit is recorded as a risk row (Pay.php:103).
	if denied == "" {
		if blocked, alert := BlockedOrderName(settings["blockname"], settings["blockalert"], p.Name); blocked {
			denied = "order name blocked"
			message = alert
		}
	}
	if denied == "" {
		if e := s.checkPaymentDomains(ctx, p); errors.Is(e, merchantpolicy.ErrPaymentDomain) {
			denied = "callback domain is not authorized"
		} else if e != nil {
			return e
		}
	}
	if denied != "" {
		s.recordRisk(ctx, p, denied)
		if message != "" {
			return PaymentRefusedError{Message: message}
		}
		return ErrForbidden
	}
	return nil
}

func (s *Service) checkPaymentDomains(ctx context.Context, p PaymentContext) error {
	if p.NotifyURL == "" && p.ReturnURL == "" {
		return nil
	}
	if in := p.OrderIntent; in != nil && in.MerchantID == p.MerchantID && in.NotifyURL == p.NotifyURL && in.ReturnURL == p.ReturnURL {
		matched, err := s.store.MatchesOrderIntent(ctx, *in)
		if err == nil && matched {
			return nil
		}
		if err != nil && !errors.Is(err, payment.ErrConflict) && !errors.Is(err, payment.ErrInvalid) {
			return err
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	policy, err := merchantpolicy.ReadPaymentDomainPolicy(ctx, tx, p.MerchantID, false)
	if err != nil {
		return err
	}
	return policy.CheckURLs(ctx, tx, p.MerchantID, false, p.NotifyURL, p.ReturnURL)
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
