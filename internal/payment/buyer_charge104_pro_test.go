package payment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestBuyerCharge104OriginalProOracle consumes actual original-calculator output,
// independently of the JavaScript formula analysis. The pinned input/output are
// external review evidence; no production material or network access is used.
func TestBuyerCharge104OriginalProOracle(t *testing.T)          { testBuyerCharge104ProOracle(t, false) }
func TestBuyerCharge104OriginalProPrecisionOracle(t *testing.T) { testBuyerCharge104ProOracle(t, true) }
func testBuyerCharge104ProOracle(t *testing.T, precision bool) {
	prefix := "buyerfees104-pro-matrix"
	inputSHA := "62fb28c6129ddeb9ed1b1c73e5b52d0aad451e1625b65772eabacb0eff89d28a"
	outputSHA := "739982f9277f2d715dd9e9e8e451dfb092c20b40d38a36a5e9774b84bcdc7fd7"
	total := 44020
	if precision {
		prefix = "buyerfees104-pro-precision"
		inputSHA = "5d9bc454ca2bfe47dc4589ca8a2d7ae7c130767491718e3efaf66a1656fff393"
		outputSHA = "a020f4f0bd21c8587a1ffbae60c35e0b79874d9962670b9bd9e25f71323c41d3"
		total = 2880
	}
	dir := os.Getenv("GOEPAY_BUYER104_ORACLE_DIR")
	if dir == "" {
		t.Skip("explicit original Pro offline oracle evidence required")
	}
	read := func(name, want string) []byte {
		t.Helper()
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != want {
			t.Fatal("original oracle evidence changed", name)
		}
		return b
	}
	// Original wire field names are deliberately decoded explicitly.
	var in struct {
		Cases []struct {
			ID       string  `json:"id"`
			Money    string  `json:"money"`
			Retained string  `json:"retained"`
			Mode     int     `json:"mode"`
			Buyer    string  `json:"buyer"`
			GT       string  `json:"global_threshold"`
			GM       string  `json:"global_minimum"`
			PT       *string `json:"parent_threshold"`
			PM       *string `json:"parent_minimum"`
		}
	}
	var out struct {
		Succeeded int  `json:"succeeded"`
		All       bool `json:"all_cases_calculated"`
		Cases     []struct {
			ID      string `json:"id"`
			Charged string `json:"charged"`
			Net     string `json:"net"`
		}
	}
	if e := json.Unmarshal(read(prefix+"-input.json", inputSHA), &in); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(read(prefix+"-output.json", outputSHA), &out); e != nil {
		t.Fatal(e)
	}
	if !out.All || out.Succeeded != total || len(in.Cases) != total || len(out.Cases) != total {
		t.Fatal("incomplete actual oracle")
	}
	hundredths := func(s string) int64 {
		t.Helper()
		parts := strings.Split(s, ".")
		if len(parts) > 2 {
			t.Fatal(s)
		}
		v, e := strconv.ParseInt(parts[0], 10, 64)
		if e != nil || v < 0 {
			t.Fatal(s, e)
		}
		fraction := "00"
		if len(parts) == 2 {
			fraction = parts[1] + "00"
			if len(parts[1]) > 2 {
				t.Fatal(s)
			}
		}
		f, e := strconv.ParseInt(fraction[:2], 10, 64)
		if e != nil {
			t.Fatal(s, e)
		}
		return v*100 + f
	}
	mismatch := 0
	for i, c := range in.Cases {
		expected := out.Cases[i]
		if expected.ID != c.ID {
			t.Fatal("oracle order changed")
		}
		min := buyerMinimum104{hundredths(c.GT), hundredths(c.GM)}
		if c.PT != nil && c.PM != nil && *c.PT != "" && *c.PM != "" {
			pt, pm := hundredths(*c.PT), hundredths(*c.PM)
			if pt > 0 && pm > 0 {
				min = buyerMinimum104{pt, pm}
			}
		}
		var buyer *string
		if c.Buyer != "" {
			buyer = &c.Buyer
		}
		actual, e := calculateBuyerChargeRate104(hundredths(c.Money), 10000-hundredths(c.Retained), c.Mode, buyer, min)
		if e != nil || actual.Charge != hundredths(expected.Charged) || actual.Net != hundredths(expected.Net) || actual.Fee != actual.Charge-actual.Net {
			mismatch++
			if mismatch <= 8 {
				t.Logf("case=%s M=%s retained=%s mode=%d buyer=%q min=%+v got=%+v err=%v original C=%s N=%s", c.ID, c.Money, c.Retained, c.Mode, c.Buyer, min, actual, e, expected.Charged, expected.Net)
			}
		}
	}
	if mismatch != 0 {
		t.Fatalf("actual original Pro oracle mismatches: %d/%d", mismatch, len(in.Cases))
	}
	t.Logf("Matched all %d actual original Pro cases; sampled decimal percentages and stated money bounds only", len(in.Cases))
}

func TestBuyerCharge104ProPartialCapsAndNetFloor(t *testing.T) {
	for _, c := range []struct {
		name             string
		m, rate, b       int64
		min              buyerMinimum104
		charge, net, fee int64
	}{
		{"six-split-three", 10000, 600, 300, buyerMinimum104{}, 10300, 9700, 600},
		{"buyer-cap-at-total", 10000, 100, 300, buyerMinimum104{}, 10100, 10000, 100},
		{"partial-rounded-zero", 100, 50, 10, buyerMinimum104{}, 100, 99, 1},
		{"minimum-net-floor", 100, 100, 10000, buyerMinimum104{10000, 500}, 200, 0, 200},
		{"minimum-partial-not-total", 10000, 600, 300, buyerMinimum104{1000, 200}, 10200, 10000, 200},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, e := calculateBuyerCharge104(c.m, c.rate, 1, &c.b, c.min)
			if e != nil || got.Charge != c.charge || got.Net != c.net || got.Fee != c.fee {
				t.Fatal(got, e)
			}
		})
	}
}
