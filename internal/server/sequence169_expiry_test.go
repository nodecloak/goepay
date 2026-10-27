package server

import (
	"context"
	"testing"
	"time"
)

func TestSequence169IndependentExpiryAfterHeadWriteMustRollback(t *testing.T) {
	s, p := groupRoutingFixture152(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var vip int64
	if e := p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"-4","type":"channel"}}}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, `CREATE FUNCTION sequence169_independent_head_delay() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.8); RETURN NEW; END $$; CREATE TRIGGER sequence169_independent_head_delay BEFORE INSERT OR UPDATE ON provider_channel_last_used FOR EACH ROW EXECUTE FUNCTION sequence169_independent_head_delay()`); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,clock_timestamp()+interval '0.5 second')`, vip); e != nil {
		t.Fatal(e)
	}
	got, _, e := s.providerRoutedRequest(ctx, 1, sequence169Params("sequence169-independent-expiry"))
	var routes, heads int
	var expired bool
	if err := p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM provider_channel_last_used),(SELECT expires_at<clock_timestamp() FROM memberships WHERE merchant_id=1)`).Scan(&routes, &heads, &expired); err != nil {
		t.Fatal(err)
	}
	if e == nil || routes != 0 || heads != 0 {
		t.Fatalf("expired membership route committed after head write: chosen=%d err=%v routes=%d heads=%d expired=%v", got, e, routes, heads, expired)
	}
}
