package riskgeo

import (
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, value string, text string) string {
	t.Helper()
	ip := netip.MustParseAddr(value)
	raw := ip.AsSlice()
	n := len(raw)
	start := 256 + 65536*8
	stride := n*2 + 6
	b := make([]byte, start+stride+len(text))
	offset := 256 + (int(raw[0])*256+int(raw[1]))*8
	binary.LittleEndian.PutUint32(b[offset:], uint32(start))
	binary.LittleEndian.PutUint32(b[offset+4:], uint32(start))
	copyIP := append([]byte{}, raw...)
	if n == 4 {
		for i := 0; i < 2; i++ {
			copyIP[i], copyIP[3-i] = copyIP[3-i], copyIP[i]
		}
	}
	copy(b[start:], copyIP)
	copy(b[start+n:], copyIP)
	binary.LittleEndian.PutUint16(b[start+n*2:], uint16(len(text)))
	binary.LittleEndian.PutUint32(b[start+n*2+2:], uint32(start+stride))
	copy(b[start+stride:], text)
	p := filepath.Join(t.TempDir(), "synthetic.xdb")
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestRisk177XDBOriginalVectorFamiliesAndBoundaries(t *testing.T) {
	for _, value := range []string{"192.0.2.177", "2001:db8::177"} {
		t.Run(value, func(t *testing.T) {
			p := fixture(t, value, "中国|测试省|测试市|0|0")
			x := XDB{p, p}
			r, found, e := x.Lookup(netip.MustParseAddr(value))
			if e != nil || !found || r.Country != "中国" || r.Province != "测试省" || r.City != "测试市" {
				t.Fatal(r, found, e)
			}
			missing := netip.MustParseAddr("203.0.113.177")
			if !netip.MustParseAddr(value).Is4() {
				missing = netip.MustParseAddr("2001:db8::178")
			}
			if _, found, e = x.Lookup(missing); e != nil || found {
				t.Fatal("range/no-vector miss", found, e)
			}
			if e = os.Truncate(p, 200); e != nil {
				t.Fatal(e)
			}
			if _, _, e = x.Lookup(netip.MustParseAddr(value)); e == nil {
				t.Fatal("truncated database accepted")
			}
		})
	}
}
func TestRisk177XDBDoesNotSubstituteMissingFamilyOrRemoteLookup(t *testing.T) {
	p := fixture(t, "192.0.2.177", "中国|0|0")
	for _, x := range []XDB{{}, {IPv4Path: p}} {
		if _, _, e := x.Lookup(netip.MustParseAddr("2001:db8::177")); e == nil {
			t.Fatal("missing v6 dataset bypassed")
		}
	}
}
