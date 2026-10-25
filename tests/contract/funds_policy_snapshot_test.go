package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestFundsActionPolicySnapshotIgnoresUnrelatedRowsWithoutTruncation(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	if _, e := p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) SELECT 'groups',jsonb_build_object('name','unrelated-group-'||n::text,'price_cents','100','enabled',true) FROM generate_series(1,1002) n;
 INSERT INTO backoffice_records(resource,owner_id,body) VALUES('merchant-risk',2,'{"merchant_id":"2","status":"normal","reason":"unrelated recipient"}');
 INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settlement_fee_rate_bps","value":"100"}')`); e != nil {
		t.Fatal(e)
	}
	target := "/console/v1/admin/workflows/settlements"
	body := json.RawMessage(`{"merchant_id":"1","amount":"2.00","external_no":"settle-snapshot"}`)
	original, _, e := s.ResolveFundsAction(ctx, p, target, body)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{name}','"unrelated-new-title"') WHERE resource='groups' AND id=(SELECT min(id) FROM backoffice_records WHERE resource='groups'); UPDATE backoffice_records SET body=jsonb_set(body,'{reason}','"unrelated-risk-change"') WHERE resource='merchant-risk' AND owner_id=2`); e != nil {
		t.Fatal(e)
	}
	unrelated, _, e := s.ResolveFundsAction(ctx, p, target, body)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(original.Digest(), unrelated.Digest()) {
		t.Error("unrelated group/other merchant invalidated target authorization")
	}
	if _, e = p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"200"') WHERE resource='settings' AND body->>'key'='settlement_fee_rate_bps'`); e != nil {
		t.Fatal(e)
	}
	changed, _, e := s.ResolveFundsAction(ctx, p, target, body)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(unrelated.Digest(), changed.Digest()) {
		t.Error("relevant fee policy hidden after 1001 unrelated records")
	}
}
