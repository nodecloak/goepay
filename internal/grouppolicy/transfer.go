package grouppolicy

import (
	"errors"

	"goepay/internal/ledger"
)

var ErrInvalidTransferFee = errors.New("invalid group transfer fee")
var ErrInvalidRedpackFeature = errors.New("invalid group redpack feature")

type TransferFeeOverride struct {
	Override      bool
	UseSettlement bool
	RateBPS       int64
}

// The reference applies PHP's falsy fallback after merging group configuration:
// literal "0" uses the effective settlement rate, while "0.00" stays zero.
func ParseTransferFeeOverride(value string) (TransferFeeOverride, error) {
	if value == "" || value == "_" {
		return TransferFeeOverride{}, nil
	}
	if value == "0" {
		return TransferFeeOverride{Override: true, UseSettlement: true}, nil
	}
	rate, err := ledger.Parse(value)
	if err != nil || rate < 0 || rate > 10000 {
		return TransferFeeOverride{}, ErrInvalidTransferFee
	}
	return TransferFeeOverride{Override: true, RateBPS: rate}, nil
}

type RedpackFeatureOverride struct {
	Override bool
	Enabled  bool
}

func ParseRedpackFeatureOverride(value string) (RedpackFeatureOverride, error) {
	switch value {
	case "", "_":
		return RedpackFeatureOverride{}, nil
	case "0", "1":
		return RedpackFeatureOverride{Override: true, Enabled: value == "1"}, nil
	default:
		return RedpackFeatureOverride{}, ErrInvalidRedpackFeature
	}
}
