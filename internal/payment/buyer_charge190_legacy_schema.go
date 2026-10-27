package payment

import "context"

// Early identity migrations predate both referral and dual-amount sources.
// Only a wholly absent, unregistered old schema may retain its original M.
// Partial loss, a modern birth flag, or a registered migration still denies.
func (s *Store) beforeReferralChargeSchema190(ctx context.Context, q referralReader, o Order) (bool, error) {
	if o.actualChargeRequired104 || o.actualChargeAmount104 != nil || o.routeFeeRequired {
		return false, nil
	}
	var absent, chargeTable, migrations bool
	var columns int
	if err := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.referral_order_terms'),to_regclass(current_schema()||'.referral_profit_costs'))=0,
 (SELECT count(*) FROM pg_attribute WHERE attrelid=to_regclass(current_schema()||'.orders') AND attname IN('referral_source_version','referral_source_hash') AND NOT attisdropped),
 to_regclass(current_schema()||'.order_customer_charges104') IS NOT NULL,to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&absent, &columns, &chargeTable, &migrations); err != nil {
		return false, err
	}
	if !absent || columns != 0 || chargeTable {
		return false, nil
	}
	if migrations {
		var registered bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name IN('000057_referral_rewards.up.sql','000071_referral_profit_costs.up.sql','000104_order_customer_charges.up.sql'))`).Scan(&registered); err != nil {
			return false, err
		}
		if registered {
			return false, ErrConflict
		}
	}
	return true, nil
}
