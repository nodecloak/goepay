package payment

import "testing"

// Original Pro notification cohorts use paid_at. Import the old creation first,
// then let the ordinary current settlement and original notification job run.
func scheduler178LegacyNotifyOrder190(t *testing.T, s *Store, name string, attempts int) Order {
	t.Helper()
	ctx := t.Context()
	if _, err := s.Merchant(ctx, 901); err != nil {
		if err = s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic notifier", APIKey: "synthetic171-only"}); err != nil {
			t.Fatal(err)
		}
	}
	var trade string
	if err := s.pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,notify_url,created_at)VALUES(nextval('trade_no_seq')::text,901,$1,'historical notifier','alipay',1000,1000,0,'none','platform','http://127.0.0.1/synthetic171',clock_timestamp()-interval '10 days')RETURNING trade_no`, "scheduler171-"+name).Scan(&trade); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPaid(ctx, trade, "synthetic171-paid-"+name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE jobs SET attempts=$2 WHERE event_key=$1`, "notify:"+trade, attempts); err != nil {
		t.Fatal(err)
	}
	o, err := s.OrderByTradeNo(ctx, trade)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
