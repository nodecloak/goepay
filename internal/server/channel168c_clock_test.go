package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/compat"
	"goepay/internal/payment"
)

func TestChannel168CCrossMidnightIncludesWholeStartAndStopHoursHTTP(t *testing.T) {
	zone := time.FixedZone("UTC+8", 8*3600)
	now := time.Now().In(zone)
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	f := channel168CClock(t, channel168Local(t), base)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=pg_catalog.clock_timestamp()+interval '48 hours'`); e != nil {
		t.Fatal(e)
	}
	channel168CSetLimits(t, f, map[string]string{"timestart": "22", "timestop": "2"})
	for i, tc := range []struct {
		offset  time.Duration
		allowed bool
	}{
		{21*time.Hour + 59*time.Minute + 59*time.Second, false},
		{22 * time.Hour, true}, {23*time.Hour + 59*time.Minute, true},
		{24 * time.Hour, true}, {26*time.Hour + 59*time.Minute + 59*time.Second, true},
		{27 * time.Hour, false},
	} {
		if _, e := f.p.Exec(ctx, `UPDATE channel168c_test_clock SET instant=$1`, base.Add(tc.offset)); e != nil {
			t.Fatal(e)
		}
		out := f.pay(t, fmt.Sprintf("cross-midnight-c-%d", i), "1.00", 7)
		if (out["code"] == float64(1)) != tc.allowed {
			t.Fatal("original whole-hour window changed", tc.offset, out)
		}
	}
}

// Only this synthetic fixture explicitly puts its schema before pg_catalog.
// Its SQL clock lets the actual PG/HTTP code cross a whole-hour boundary after
// the last write without adding a production clock hook or waiting an hour.
func channel168CClock(t *testing.T, f channel168Fixture, instant time.Time) channel168Fixture {
	t.Helper()
	ctx := context.Background()
	var schema string
	if e := f.p.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(ctx, `CREATE TABLE channel168c_test_clock(instant timestamptz NOT NULL);CREATE FUNCTION clock_timestamp() RETURNS timestamptz LANGUAGE sql VOLATILE AS 'SELECT instant FROM channel168c_test_clock'`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(ctx, `INSERT INTO channel168c_test_clock VALUES($1)`, instant); e != nil {
		t.Fatal(e)
	}
	config := f.p.Config()
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",pg_catalog"
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.p = pool
	f.s.Pool = pool
	f.s.Store = store
	f.s.ProviderFlow.Store = store
	var actual time.Time
	if e = pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&actual); e != nil || !actual.Equal(instant) {
		t.Fatal("synthetic SQL clock not installed", actual, e)
	}
	return f
}

func TestChannel168CLastRouteAndBirthWriteCannotCommitAfterParentHoursClose(t *testing.T) {
	for _, target := range []string{"provider_route_fee_quotes", "order_route_fee_terms"} {
		t.Run(target, func(t *testing.T) {
			f := channel168Local(t)
			ctx := context.Background()
			local := time.Now().In(time.FixedZone("UTC+8", 8*3600))
			channel168CSetLimits(t, f, map[string]string{"timestart": fmt.Sprint((local.Hour() + 22) % 24), "timestop": fmt.Sprint(local.Hour())})
			nearEnd := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 59, 59, 500000000, local.Location())
			f = channel168CClock(t, f, nearEnd)
			if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info}','{"alipay":{"channel":"-4","type":"channel"}}') WHERE resource='groups' AND id=0`); e != nil {
				t.Fatal(e)
			}
			if _, e := f.p.Exec(ctx, `CREATE SEQUENCE channel168c_clock_entered;CREATE FUNCTION channel168c_end_hours() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('channel168c_clock_entered');UPDATE channel168c_test_clock SET instant=date_trunc('hour',instant)+interval '1 hour 1 millisecond';RETURN NEW;END$$;CREATE TRIGGER channel168c_end_hours AFTER INSERT ON `+target+` FOR EACH ROW EXECUTE FUNCTION channel168c_end_hours()`); e != nil {
				t.Fatal(e)
			}
			p := compat.Params{"type": "alipay", "method": "native", "name": "last clock c", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "last-clock-c", "channel_id": "7"}
			_, _, e := f.s.providerRoutedRequest(ctx, 1, p)
			if target == "order_route_fee_terms" {
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.s.Store.CreateOrder(ctx, create168(p, 100))
			}
			if e == nil {
				t.Fatal("global parent closing hour passed while last write committed", target)
			}
			var entered bool
			if e = f.p.QueryRow(ctx, `SELECT is_called FROM channel168c_clock_entered`).Scan(&entered); e != nil || !entered {
				t.Fatal("last-write clock gate not reached", e)
			}
			var routes, quotes, orders, terms, heads int
			if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM orders),(SELECT count(*) FROM order_route_fee_terms),(SELECT count(*) FROM provider_channel_last_used)`).Scan(&routes, &quotes, &orders, &terms, &heads); e != nil {
				t.Fatal(e)
			}
			expected := 0
			if target == "order_route_fee_terms" {
				expected = 1
			}
			if routes != expected || quotes != expected || heads != expected || orders != 0 || terms != 0 {
				t.Fatal("hour expiry failed transaction-wide rollback", target, routes, quotes, orders, terms, heads)
			}
		})
	}
}
