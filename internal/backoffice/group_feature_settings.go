package backoffice

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

func init() {
	d := definitions["groups"]
	labels := map[string]string{"recharge": "余额充值", "onecode": "聚合收款码", "subchannel_open": "子通道管理功能", "user_profitsharing": "分账规则管理功能", "user_transfer_batch": "批量代付功能"}
	for _, key := range merchantpolicy.GroupFeatureKeys {
		field := selectField(key, labels[key], "", "_", "0", "1")
		field.Required = false
		field.Options = []Option{{Label: "缺省（与系统设置一致）", Value: ""}, {Label: "缺省（保留原值）", Value: "_"}, {Label: "关闭", Value: "0"}, {Label: "开启", Value: "1"}}
		d.fields = append(d.fields, field)
	}
	definitions["groups"] = d
}

func validateGroupFeatureSettings(values map[string]any) error {
	body, err := json.Marshal(values)
	if err != nil {
		return ErrInvalid
	}
	for _, key := range merchantpolicy.GroupFeatureKeys {
		if _, err := merchantpolicy.GroupFeatureSwitch(body, key); err != nil {
			return ErrInvalid
		}
	}
	return nil
}

// MerchantFeatureVisibleForMerchant resolves a current effective group only for
// the business-feature keys above. Other keys keep their existing global policy.
func (s *Service) MerchantFeatureVisibleForMerchant(ctx context.Context, mid int64, key string) (bool, error) {
	if !merchantpolicy.GroupFeatureKey(key) {
		return s.MerchantFeatureVisible(ctx, key)
	}
	features, err := s.merchantFeatureVisibility(ctx, mid, []string{key}, true)
	return features[key], err
}

// MerchantFeaturesForMerchant keeps the menu snapshot coherent while avoiding a
// separate group/global database round trip for every feature in the session.
func (s *Service) MerchantFeaturesForMerchant(ctx context.Context, mid int64) (map[string]bool, error) {
	return s.merchantFeatureVisibility(ctx, mid, MerchantFeatureVisibleKeys, false)
}

func (s *Service) merchantFeatureVisibility(ctx context.Context, mid int64, keys []string, strict bool) (map[string]bool, error) {
	out := map[string]bool{}
	if mid <= 0 {
		return out, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	group, err := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, false)
	groupUnavailable := errors.Is(err, merchantpolicy.ErrGroupPolicy)
	if err != nil && (strict || !groupUnavailable) {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id`, keys)
	if err != nil {
		return out, err
	}
	values := map[string]string{}
	nulls := map[string]bool{}
	for rows.Next() {
		var key string
		var value *string
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			return out, err
		}
		if _, exists := values[key]; !exists {
			if value == nil {
				values[key], nulls[key] = "", true
			} else {
				values[key] = *value
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	for _, key := range keys {
		value, exists := values[key]
		if nulls[key] && !merchantpolicy.GroupFeatureKey(key) {
			return out, ErrInvalid
		}
		enabled := value != "0"
		if merchantpolicy.GroupFeatureKey(key) {
			if groupUnavailable || exists && value != "0" && value != "1" {
				if strict {
					return out, merchantpolicy.ErrFeatureSwitch
				}
				out[key] = false
				continue
			}
			override, err := merchantpolicy.GroupFeatureSwitch(group.Body, key)
			if err != nil {
				if strict {
					return out, err
				}
				out[key] = false
				continue
			}
			if override.Configured {
				enabled = override.Enabled
			}
		}
		out[key] = enabled
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// Keep old code records readable and removable after closing the feature. New
// or edited collection records must not bypass the generator's matching gate.
func (s *Service) checkGroupResourceFeature(ctx context.Context, p Principal, resource string) error {
	if p.Role != "merchant" || resource != "collection-codes" {
		return nil
	}
	on, err := s.MerchantFeatureVisibleForMerchant(ctx, p.SubjectID, "onecode")
	if err != nil {
		return err
	}
	if !on {
		return FeatureClosed("未开启聚合收款码功能")
	}
	return nil
}
