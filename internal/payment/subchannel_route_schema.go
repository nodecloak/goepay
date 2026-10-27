package payment

import (
	"context"
)

// Absence alone is not historical provenance: current migration or an original
// sealed child quote prevents falling back to the parent when child tables vanish.
func (s *Store) childSchemaPresent(ctx context.Context, q fundsReader, mid int64, no string) (bool, error) {
	var present, migrations, quotes bool
	if e := q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.payment_subchannel_routes') IS NOT NULL,to_regclass(current_schema()||'.schema_migrations') IS NOT NULL,to_regclass(current_schema()||'.provider_route_fee_quotes') IS NOT NULL`).Scan(&present, &migrations, &quotes); e != nil {
		return false, e
	}
	if present {
		return true, nil
	}
	if migrations {
		var current bool
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000098_merchant_payment_subchannels.up.sql')`).Scan(&current); e != nil {
			return false, e
		}
		if current {
			return false, ErrConflict
		}
	}
	if quotes {
		var required bool
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_route_fee_quotes WHERE merchant_id=$1 AND ($2::text='' OR out_trade_no=$2) AND facts ? 'child_execution')`, mid, no).Scan(&required); e != nil {
			return false, e
		}
		if required {
			return false, ErrConflict
		}
	}
	return false, nil
}
