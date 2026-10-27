package server

import (
	"goepay/internal/riskgeo"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestRisk177PinnedOfficialGeoDrivesActualAdmissionAndScan(t *testing.T) {
	dir := os.Getenv("GOEPAY_RISK177_OFFICIAL_GEO_DIR")
	if dir == "" {
		t.Skip("optional official offline data fixture directory required")
	}
	x := riskgeo.XDB{IPv4Path: filepath.Join(dir, "ip2region_v4.xdb"), IPv6Path: filepath.Join(dir, "ip2region_v6.xdb")}
	f, _, creates := risk177Flow(t)
	f.s.Store.CoreRiskGeo = x
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["pay_block_oversea"] = "1"; s["check_pay_regoin"] = "1" }), f.csrf))
	domestic := f.pay(t, "risk177-official-domestic", "223.5.5.5")
	if domestic["code"] != float64(1) || creates.Load() != 1 {
		t.Fatal("real data domestic positive rejected", domestic["code"])
	}
	if foreign := f.pay(t, "risk177-official-foreign", "8.8.8.8"); foreign["code"] == float64(1) || creates.Load() != 1 {
		t.Fatal("real data foreign admission created payment")
	}
	a, _, e := x.Lookup(netip.MustParseAddr("223.5.5.5"))
	if e != nil {
		t.Fatal(e)
	}
	b, _, e := x.Lookup(netip.MustParseAddr("8.8.8.8"))
	if e != nil {
		t.Fatal(e)
	}
	want := 403
	if a.Province == "" || a.City == "" || b.Province == "" || b.City == "" {
		want = 503
	} else if a.Province == b.Province && a.City == b.City {
		t.Fatal("independent real data samples not different regions")
	}
	if scan := risk177ScanGET(f, f.s.checkoutURL(domestic["trade_no"].(string), "scan-region"), "8.8.8.8"); scan.Code != want {
		t.Fatal("real XDB scan result not enforced", scan.Code, want)
	}
}
