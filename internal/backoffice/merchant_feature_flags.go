package backoffice

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// FeatureClosedError is returned when a merchant hits a feature whose toggle is
// off. It carries the original merchant-facing message (e.g. "未开启余额充值功能")
// and is surfaced by writeError as a 403 with that message, matching the
// original user-side `exit('...')` / menu-hidden behaviour.
type FeatureClosedError struct{ Message string }

func (e FeatureClosedError) Error() string { return e.Message }

// FeatureClosed builds a FeatureClosedError with the given merchant-facing text.
func FeatureClosed(message string) error { return FeatureClosedError{Message: message} }

// MerchantFeatureEnabled reports whether a merchant-center feature toggle
// (internal_transfer / recharge / onecode / complain_open / applyments_open /
// mchrisk_open) is on. A missing or malformed settings row means off, matching
// the original where an unset $conf value leaves the feature disabled. The
// merchant console hides the menu entry and the matching workflow endpoint
// refuses the request when the toggle is off; administrator management paths
// are not gated by it.
func (s *Service) MerchantFeatureEnabled(ctx context.Context, key string) (bool, error) {
	known := false
	for _, k := range MerchantFeatureKeys {
		if k == key {
			known = true
		}
	}
	if !known {
		return false, ErrInvalid
	}
	value, err := s.settingValue(ctx, key)
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

// MerchantFeatureVisibleKeys are the merchant-center features that default ON:
// the original ships them enabled, so a missing settings row leaves the menu
// entry shown and the endpoint open, and only an explicit "0" hides the menu
// and refuses the workflow endpoint. internal_transfer / recharge / onecode
// use this default-ON gate; complain_open / applyments_open / mchrisk_open use
// MerchantFeatureEnabled (default off, surfaced only when turned on). The
// wave-2 switches in MerchantSwitchDefaultOnKeys are default-ON too.
var MerchantFeatureVisibleKeys = append([]string{"internal_transfer", "recharge", "onecode"}, MerchantSwitchDefaultOnKeys...)

// MerchantFeatureVisible reports whether a default-ON merchant feature is
// enabled: true unless the stored value is exactly "0". A missing row means ON,
// preserving today's contract (recharge/internal-transfer/onecode endpoints are
// available by default and covered by sibling tests) until an administrator
// turns the feature off on the settings page.
func (s *Service) MerchantFeatureVisible(ctx context.Context, key string) (bool, error) {
	known := false
	for _, k := range MerchantFeatureVisibleKeys {
		if k == key {
			known = true
		}
	}
	if !known {
		return false, ErrInvalid
	}
	value, err := s.settingValue(ctx, key)
	if err != nil {
		return false, err
	}
	return value != "0", nil
}

// settingValue reads a single settings row's stored string value; a missing row
// returns "" with no error, matching the original where an unset $conf key is
// an empty string. Shared by the feature gates and the pay-guard readers below.
func (s *Service) settingValue(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1 ORDER BY id LIMIT 1`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// SettingValue is the exported single-key reader for callers outside the
// backoffice package (payment create / checkout paths). A missing row yields "".
func (s *Service) SettingValue(ctx context.Context, key string) (string, error) {
	return s.settingValue(ctx, key)
}

// SettingValues reads several settings rows at once, returning a key->value map
// with only the rows that exist (missing keys absent, read as "" by the
// decision helpers). Used to resolve the settle_*/transfer_* channel switches
// and the pay-guard texts together.
func (s *Service) SettingValues(ctx context.Context, keys ...string) (map[string]string, error) {
	out := map[string]string{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1) ORDER BY id`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if _, seen := out[k]; !seen {
			out[k] = v
		}
	}
	return out, rows.Err()
}
