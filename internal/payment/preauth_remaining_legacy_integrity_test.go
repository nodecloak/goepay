package payment

import (
	"context"
	"errors"
	"testing"
)

func TestPreauthRemaining75LegacyReleaseCannotSubstituteOriginalAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		value       any
	}{{"foreign auth", "auth_no", "different-authorization"}, {"partial amount without source", "amount", int64(60)}, {"different channel", "channel_id", int64(8)}} {
		t.Run(tc.name, func(t *testing.T) {
			s, o := releaseTestOrder(t)
			ctx := context.Background()
			if _, owner, e := s.BeginAuthorizationRelease(ctx, o.MerchantID, o.TradeNo); e != nil || !owner {
				t.Fatal(owner, e)
			}
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
				t.Fatal(e)
			}
			// The column name comes solely from the closed table above.
			if _, e = tx.Exec(ctx, "UPDATE payment_authorization_releases SET "+tc.field+"=$1 WHERE order_id=$2", tc.value, o.ID); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e = s.AuthorizationRelease(ctx, o.MerchantID, o.TradeNo); !errors.Is(e, ErrConflict) {
				t.Fatal("legacy release adopted unrelated original fields", e)
			}
			if e = s.MarkAuthorizationReleaseSent(ctx, o.TradeNo); !errors.Is(e, ErrConflict) {
				t.Fatal("damaged original release acquired first send", e)
			}
			if e = s.ResolveAuthorizationRelease(ctx, o.TradeNo, "unknown"); !errors.Is(e, ErrConflict) {
				t.Fatal("damaged release acquired a new result state", e)
			}
		})
	}
}
