package server

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSubchannel168BParent169OldValidSourceCannotRewindGlobalHead(t *testing.T) {
	s, p, _ := sequence169Fixture(t)
	s.MerchantChannels[1] = []int64{7, 8}
	ctx := context.Background()
	for i, want := range []int64{7, 8, 7} {
		sequence169Route(t, s, 1, fmt.Sprintf("parent168b-%d", i), want)
		if i == 0 {
			if _, e := p.Exec(ctx, `CREATE TABLE subchannel168b_old_parent_head AS SELECT * FROM provider_channel_last_used WHERE channel_id=7`); e != nil {
				t.Fatal(e)
			}
		}
		time.Sleep(4 * time.Millisecond)
	}
	var history int
	if e := p.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&history); e != nil || history != 3 {
		t.Fatal(history, e)
	}
	if _, e := p.Exec(ctx, `UPDATE provider_channel_last_used h SET (merchant_id,out_trade_no,last_used_millis)=(SELECT merchant_id,out_trade_no,last_used_millis FROM subchannel168b_old_parent_head o WHERE o.channel_id=h.channel_id) WHERE channel_id=7`); e != nil {
		t.Fatal("ordinary old valid source must pass old FK/source guard so reader is tested", e)
	}
	got, _, e := s.providerRoutedRequest(ctx, 1, sequence169Params("parent168b-after-rewind"))
	if e == nil {
		t.Fatal("old legal parent source rewound actual -4 selection", got)
	}
	var after int
	p.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&after)
	if after != history {
		t.Fatal("rejected old-head request wrote an assignment")
	}
}
func TestSubchannel168BParent169IgnoresLaterNonSequentialAssignment(t *testing.T) {
	s, p, groups := sequence169Fixture(t)
	s.MerchantChannels[1] = []int64{7, 8}
	ctx := context.Background()
	sequence169Route(t, s, 1, "parent168b-mode-first", 7)
	time.Sleep(4 * time.Millisecond)
	sequence169Route(t, s, 1, "parent168b-mode-second", 8)
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"7","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=$1`, groups[0]); e != nil {
		t.Fatal(e)
	}
	sequence169Route(t, s, 1, "parent168b-direct-later", 7)
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"-4","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=$1`, groups[0]); e != nil {
		t.Fatal(e)
	}
	sequence169Route(t, s, 1, "parent168b-mode-back", 7)
}
