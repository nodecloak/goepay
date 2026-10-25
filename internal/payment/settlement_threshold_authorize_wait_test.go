package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSettlementThreshold74AuthorizationCannotCommitAfterGrantWaitExpiresOriginalWindow(t *testing.T) {
	for _, mode := range []string{"generate_only", settlementThresholdMode} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			p := settlementPlanReady(t, s, 7434)
			if mode == settlementThresholdMode {
				in := settlementPlanInputFor(7434)
				in.Mode = mode
				var e error
				p, e = s.UpdateSettlementPlan(ctx, p.ID, p.Revision, in, settlementPlanAdmin)
				if e != nil {
					t.Fatal(e)
				}
				transferSetting(t, s, "auto_settle_money", "1.00")
				transferSetting(t, s, "transfer_alipay", "7")
			}
			if _, e := s.pool.Exec(ctx, "CREATE TABLE local_authorize_wait74(action_key text PRIMARY KEY)"); e != nil {
				t.Fatal(e)
			}
			input := settlementPlanAuthorizationInput(t, s)
			input.ExpiresAt = input.ValidFrom.Add(time.Minute + 10*time.Second)
			reached := false
			guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
				if kind != "settlement-plan-authorize" {
					return ErrInvalid
				}
				reached = true
				if _, e := tx.Exec(c, "INSERT INTO local_authorize_wait74(action_key)VALUES($1)", key); e != nil {
					return e
				}
				_, e := tx.Exec(c, "SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.1)", input.ExpiresAt)
				return e
			})
			if _, e := s.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, input, settlementPlanAdmin); !errors.Is(e, ErrState) {
				t.Fatal("expired original grant window enabled plan", e)
			}
			if !reached {
				t.Fatal("fixture never reached transactional grant wait")
			}
			var authorities, grants int
			var enabled bool
			if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_plan_authorizations),(SELECT count(*) FROM local_authorize_wait74),(SELECT enabled FROM settlement_plans WHERE id=$1)", p.ID).Scan(&authorities, &grants, &enabled); e != nil || authorities != 0 || grants != 0 || enabled {
				t.Fatal("expired authorization consumed a grant or became durable", authorities, grants, enabled, e)
			}
		})
	}
}
