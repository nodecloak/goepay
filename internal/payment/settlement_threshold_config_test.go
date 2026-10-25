package payment

import (
	"context"
	"errors"
	"testing"
)

func TestSettlementThreshold74ConfigDefaultsClosedAndReadsOnlyOriginalBranchKeys(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	initial, e := readSettlementThresholdConfig(ctx, s.pool, false)
	if e != nil || initial.Enabled || initial.ChannelID != 0 || initial.Policy.ThresholdCents != 0 {
		t.Fatal("missing original threshold enabled payments", initial, e)
	}
	for key, value := range map[string]string{"auto_settle_money": "100.25", "transfer_alipay": "7", "settlement_fee_rate_bps": "300", "settlement_fee_maximum_cents": "250", "settle_type": "1", "settlement_fee_minimum_cents": "invalid-not-used-by-Transfer", "settlement_retained_cents": "invalid-not-used-by-Transfer"} {
		transferSetting(t, s, key, value)
	}
	got, e := readSettlementThresholdConfig(ctx, s.pool, false)
	if e != nil || !got.Enabled || got.ChannelID != 7 || got.Policy.ThresholdCents != 10025 || got.Policy.FeeRateBPS != 300 || got.Policy.FeeMaximumCents != 250 || got.SettleType != "1" || len(got.Facts) != 5 {
		t.Fatal("threshold incorrectly inherited normal Settle fields or units", got, e)
	}
	transferSetting(t, s, "auto_settle_money", "0")
	stopped, e := readSettlementThresholdConfig(ctx, s.pool, false)
	if e != nil || stopped.Enabled {
		t.Fatal("explicit zero threshold did not close", stopped, e)
	}
	if _, e = readSettlementThresholdConfig(ctx, nil, false); !errors.Is(e, ErrInvalid) {
		t.Fatal("nil reader accepted", e)
	}
}

func TestSettlementThreshold74ConfigRejectsAmbiguousMoneyChannelAndFeeBounds(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, c := range []struct{ key, value string }{{"auto_settle_money", "-1.00"}, {"auto_settle_money", "1e2"}, {"auto_settle_money", "100.001"}, {"auto_settle_money", "1000000000.01"}, {"transfer_alipay", "07"}, {"transfer_alipay", "-1"}, {"settlement_fee_rate_bps", "10001"}, {"settlement_fee_maximum_cents", "-1"}, {"settle_type", "2"}} {
		transferSetting(t, s, c.key, c.value)
		if _, e := readSettlementThresholdConfig(ctx, s.pool, false); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid original threshold config accepted", c.key, e)
		}
		if _, e := s.pool.Exec(ctx, "DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1", c.key); e != nil {
			t.Fatal(e)
		}
	}
}
