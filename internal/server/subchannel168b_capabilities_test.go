package server

import (
	"context"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"testing"
)

func TestSubchannel168BOrderActionsUseEachOriginalChildCapabilitiesInOnePage(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	factory.providers["no-query-child"] = &channel168Provider{app: "no-query-child", subchannelTestCaps: &adapter.Capabilities{Methods: []string{"native"}}}
	row := child168BCreate(t, f, "no-query-child", true)
	if out := f.pay(t, "caps168b-disabled", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	// Rotate the same child to a query-capable new account; old attempts retain theirs.
	w := f.childHTTP("PATCH", "/"+row["id"].(string), fmt.Sprintf(`{"expected_fingerprint":%q,"values":{"app":{"operation":"replace","value":"query-child"}}}`, row["fingerprint"]))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if out := f.pay(t, "caps168b-enabled", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	a, e := f.s.Store.Order(context.Background(), 1, "caps168b-disabled")
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.s.Store.Order(context.Background(), 1, "caps168b-enabled")
	if e != nil {
		t.Fatal(e)
	}
	items := []map[string]any{{"id": fmt.Sprint(a.ID)}, {"id": fmt.Sprint(b.ID)}}
	if e = f.s.merchantOrderActions(context.Background(), identity.Session{User: identity.User{ID: 1, Role: "merchant"}}, items); e != nil {
		t.Fatal(e)
	}
	if items[0]["merchant_actions"].(map[string]bool)["query"] || !items[1]["merchant_actions"].(map[string]bool)["query"] {
		t.Fatal("page borrowed parent or another row account capabilities", items)
	}
}
