package payment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// PaymentCostEstimate is private server configuration, never a provider bill
// or a merchant-supplied payment parameter.
type PaymentCostEstimate struct {
	Provider, AppID, AccountScope, Revision string `json:"-"`
	RateUnits                               int64  `json:"-"`
}

type PaymentCostEstimator interface {
	PaymentCostEstimate() (PaymentCostEstimate, bool)
}

// NewPaymentCostEstimate only accepts an identified physical receiving account.
// In particular Alipay's application-only fallback is not a seller identity.
func NewPaymentCostEstimate(p Provider, raw string) (PaymentCostEstimate, error) {
	var out PaymentCostEstimate
	rate, err := ParsePaymentCostRate(raw)
	if err != nil || p == nil {
		return out, ErrInvalid
	}
	app, ok := p.(ApplicationIdentity)
	if !ok || app.ApplicationID() == "" {
		return out, ErrInvalid
	}
	account, ok := p.(TransferAccountIdentity)
	if !ok || account.TransferAccountIdentity() == "" {
		return out, ErrInvalid
	}
	_, plugin := p.(PluginIdentity)
	switch p.ID() {
	case "wxpay":
	case "alipay":
		if !plugin && (!strings.HasPrefix(account.TransferAccountIdentity(), "seller:") || len(account.TransferAccountIdentity()) <= len("seller:")) {
			return out, ErrInvalid
		}
	default:
		// Ported plugins identify the upstream merchant account themselves.
		if !plugin {
			return out, ErrInvalid
		}
	}
	scope, err := TransferAccountScope(p)
	if err != nil {
		return out, err
	}
	out = PaymentCostEstimate{Provider: p.ID(), AppID: app.ApplicationID(), AccountScope: scope, RateUnits: rate.Units}
	rawRevision, _ := json.Marshal([]any{"configured-estimate-percent-v1", out.Provider, out.AppID, out.AccountScope, rate.Canonical})
	hash := sha256.Sum256(rawRevision)
	out.Revision = hex.EncodeToString(hash[:])
	return out, nil
}

// PaymentCostRate is a configured estimate in percent, with six fractional
// decimal places. It is not evidence of a provider's billed expense.
type PaymentCostRate struct {
	Units     int64  `json:"-"`
	Canonical string `json:"-"`
}

func ParsePaymentCostRate(raw string) (PaymentCostRate, error) {
	var out PaymentCostRate
	if len(raw) == 0 || len(raw) > 10 {
		return out, ErrInvalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 3 || len(parts[0]) > 1 && parts[0][0] == '0' {
		return out, ErrInvalid
	}
	for _, part := range parts {
		if len(part) == 0 {
			return out, ErrInvalid
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return out, ErrInvalid
			}
		}
	}
	whole, _ := strconv.ParseInt(parts[0], 10, 64)
	units := whole * 1000000
	if len(parts) == 2 {
		if len(parts[1]) > 6 {
			return out, ErrInvalid
		}
		fraction, _ := strconv.ParseInt(parts[1]+strings.Repeat("0", 6-len(parts[1])), 10, 64)
		units += fraction
	}
	if units > 100000000 {
		return out, ErrInvalid
	}
	out.Units = units
	out.Canonical = parts[0]
	if len(parts) == 2 {
		fraction := strings.TrimRight(parts[1], "0")
		if fraction != "" {
			out.Canonical += "." + fraction
		}
	}
	return out, nil
}
