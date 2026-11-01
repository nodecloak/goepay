package merchantpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrPaymentDomain = errors.New("callback domain is not authorized")
var ErrPaymentDomainPolicy = errors.New("invalid merchant payment domain policy")
var ErrPaymentDomainChanged = errors.New("merchant payment domain policy changed or expired")

type PaymentDomainOverride struct{ Override, Enabled bool }

func ParsePaymentDomainOverride(value string) (PaymentDomainOverride, error) {
	switch value {
	case "", "_":
		return PaymentDomainOverride{}, nil
	case "0", "1":
		return PaymentDomainOverride{Override: true, Enabled: value == "1"}, nil
	default:
		return PaymentDomainOverride{}, ErrPaymentDomainPolicy
	}
}

type PaymentDomainPolicy struct {
	Enabled          bool
	GroupID          int64
	ValidUntil       time.Time
	override         bool
	globalConfigured bool
}

// New payment admission uses the caller's transaction and locks. Historical
// intents deliberately return before this current policy is read.
func ReadPaymentDomainPolicy(ctx context.Context, q Reader, mid int64, lock bool) (PaymentDomainPolicy, error) {
	var out PaymentDomainPolicy
	group, err := ReadEffectiveGroup(ctx, q, mid, lock)
	if err != nil {
		return out, err
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(group.Body, &values) != nil {
		return out, ErrPaymentDomainPolicy
	}
	value := ""
	if raw, exists := values["pay_domain_forbid"]; exists && string(raw) != "null" {
		if json.Unmarshal(raw, &value) != nil {
			return out, ErrPaymentDomainPolicy
		}
	}
	parsed, err := ParsePaymentDomainOverride(value)
	if err != nil {
		return out, err
	}
	out.GroupID, out.ValidUntil, out.override = group.ID, group.ValidUntil, parsed.Override
	if parsed.Override {
		out.Enabled = parsed.Enabled
		return out, nil
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var raw json.RawMessage
	err = q.QueryRow(ctx, `SELECT body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='enforce_domains' ORDER BY id LIMIT 1`+suffix).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.globalConfigured = true
	if json.Unmarshal(raw, &value) != nil || string(raw) == "null" {
		return out, ErrPaymentDomainPolicy
	}
	switch value {
	case "", "false":
	case "true":
		out.Enabled = true
	default:
		return out, ErrPaymentDomainPolicy
	}
	return out, nil
}

func (p PaymentDomainPolicy) Recheck(ctx context.Context, q Reader, mid int64) error {
	if err := p.CheckDeadline(ctx, q); err != nil {
		return err
	}
	current, err := ReadPaymentDomainPolicy(ctx, q, mid, true)
	if err != nil {
		return err
	}
	if p.Enabled != current.Enabled || p.GroupID != current.GroupID || p.override != current.override || p.globalConfigured != current.globalConfigured || !p.ValidUntil.Equal(current.ValidUntil) {
		return ErrPaymentDomainChanged
	}
	return current.CheckDeadline(ctx, q)
}

func (p PaymentDomainPolicy) CheckDeadline(ctx context.Context, q Reader) error {
	if p.ValidUntil.IsZero() {
		return nil
	}
	var at time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return err
	}
	if !at.Before(p.ValidUntil) {
		return ErrPaymentDomainChanged
	}
	return nil
}

// Both callback destinations require the merchant's own current enabled,
// reviewed domain. This does not replace URL/SSRF validation at wire and I/O.
func (p PaymentDomainPolicy) CheckURLs(ctx context.Context, q Reader, mid int64, lock bool, urls ...string) error {
	if !p.Enabled {
		return nil
	}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	rows, err := q.Query(ctx, `SELECT body->>'domain' FROM backoffice_records WHERE resource='domains' AND owner_id=$1 AND body->>'approved'='true' AND body->>'enabled'='true' ORDER BY id`+suffix, mid)
	if err != nil {
		return err
	}
	domains := []string{}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		domains = append(domains, value)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, raw := range urls {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil {
			return ErrPaymentDomain
		}
		matched := false
		for _, rule := range domains {
			if AuthorizationDomainMatches(u.Hostname(), rule) {
				matched = true
				break
			}
		}
		if !matched {
			return ErrPaymentDomain
		}
	}
	return nil
}

var authorizationDomainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func NormalizeAuthorizationDomain(value string) (string, bool) {
	value = strings.TrimSuffix(strings.ToLower(value), ".")
	base := strings.TrimPrefix(value, "*.")
	if len(value) > 253 || !authorizationDomainPattern.MatchString(base) || net.ParseIP(base) != nil {
		return "", false
	}
	return value, true
}

func AuthorizationDomainMatches(host, rule string) bool {
	if strings.Contains(host, "*") {
		return false
	}
	host, validHost := NormalizeAuthorizationDomain(host)
	rule, validRule := NormalizeAuthorizationDomain(rule)
	if !validHost || !validRule {
		return false
	}
	if strings.HasPrefix(rule, "*.") {
		base := strings.TrimPrefix(rule, "*.")
		return host != base && strings.HasSuffix(host, "."+base)
	}
	return host == rule || strings.HasSuffix(host, "."+rule)
}
