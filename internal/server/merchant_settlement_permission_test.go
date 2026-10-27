package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestOriginalMerchantSettlementPermissionAPIUsesActualOriginalFlag(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	for _, tc := range []struct {
		profile string
		want    int
	}{{`{}`, 1}, {`{"settle":0,"settle_status":1}`, 0}, {`{"settle":1,"settle_status":0}`, 1}, {`{"settle_status":0}`, 0}} {
		if _, e := pool.Exec(ctx, "UPDATE merchants SET profile=$1::jsonb WHERE id=1", tc.profile); e != nil {
			t.Fatal(e)
		}
		w := httptest.NewRecorder()
		s.merchantInfo(w, httptest.NewRequest("POST", "/api/getMerchant", nil), false, 1)
		var data map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil || data["code"] != float64(0) || data["settle_status"] != float64(tc.want) {
			t.Fatalf("merchant status contradicts current original permission %s %s %v", tc.profile, w.Body.String(), e)
		}
	}
}

func TestOriginalMerchantSettlementPermissionToggleInvalidatesExactFundsAction(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{settle}','1'::jsonb) WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	body := json.RawMessage(`{"merchant_id":"1","external_no":"settle160-grant","amount":"1.00"}`)
	before, _, e := s.ResolveFundsAction(ctx, pool, "/console/v1/admin/workflows/settlements", body)
	if e != nil {
		t.Fatal(e)
	}
	var originalProfile, afterProfile string
	var originalRevision, afterRevision int64
	if e = pool.QueryRow(ctx, "SELECT profile::text,config_revision FROM merchants WHERE id=1").Scan(&originalProfile, &originalRevision); e != nil {
		t.Fatal(e)
	}
	for _, value := range []string{"0", "1"} {
		if _, e = pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_set(profile,'{settle}',$1::jsonb) WHERE id=1", value); e != nil {
			t.Fatal(e)
		}
	}
	if e = pool.QueryRow(ctx, "SELECT profile::text,config_revision FROM merchants WHERE id=1").Scan(&afterProfile, &afterRevision); e != nil || originalProfile != afterProfile || originalRevision != afterRevision {
		t.Fatal("fixture did not restore the exact prior business profile", e)
	}
	after, _, e := s.ResolveFundsAction(ctx, pool, "/console/v1/admin/workflows/settlements", body)
	if e != nil {
		t.Fatal(e)
	}
	if before == after {
		t.Fatal("original funds confirmation revived after permission off/on")
	}
}
