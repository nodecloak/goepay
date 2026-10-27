package riskgeo

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestRisk177PinnedOfficialIPv4IPv6OfflineQueries(t *testing.T) {
	dir := os.Getenv("GOEPAY_RISK177_OFFICIAL_GEO_DIR")
	if dir == "" {
		t.Skip("optional pinned official data fixture directory required; no network during tests")
	}
	x := XDB{IPv4Path: filepath.Join(dir, "ip2region_v4.xdb"), IPv6Path: filepath.Join(dir, "ip2region_v6.xdb")}
	if !x.Ready() {
		t.Fatal("both official local XDB files must be ready")
	}
	results := map[string]Region{}
	for _, ip := range []string{"223.5.5.5", "114.114.114.114", "8.8.8.8", "2001:4860:4860::8888", "240e::1"} {
		r, found, e := x.Lookup(netip.MustParseAddr(ip))
		if e != nil || !found || r.Country == "" {
			t.Fatal("official offline query failed", ip, found, e)
		}
		results[ip] = r
		t.Log(ip, r.Country, r.Province, r.City)
	}
	if results["223.5.5.5"].Country != "中国" && results["223.5.5.5"].Country != "China" {
		t.Fatal("official domestic sample not classified as China")
	}
	if results["8.8.8.8"].Country == results["223.5.5.5"].Country {
		t.Fatal("official foreign sample collapsed into domestic")
	}
	if out := os.Getenv("GOEPAY_RISK177_OFFICIAL_GEO_RESULT"); out != "" {
		raw, e := json.MarshalIndent(map[string]any{"scope": "real pinned public XDB data; offline lookup only, not geographic accuracy certification", "queries": results}, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(out, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
