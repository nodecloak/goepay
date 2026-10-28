package payment

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

//go:embed buyer_charge104_release.php
var originalBuyerFee104PHP string

func TestOriginalBuyerCharge104ReadableReleasePHPInterop(t *testing.T) {
	testphp.Require(t)
	reference, e := filepath.Abs(testphp.Path(t, "epay_release/includes/lib/api/Pay.php"))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(reference)
	if e != nil {
		t.Fatal(e)
	}
	const originalSHA = "083e4a941d734f85df3eb6d98b1aec873e8090f196af91c4c694a9c31c73a0f6"
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != originalSHA {
		t.Fatal("original Release fee source changed")
	}
	type sample struct {
		Original  int64 `json:"original"`
		FeeBPS    int64 `json:"fee_bps"`
		Mode      int   `json:"mode"`
		Threshold int64 `json:"threshold"`
		Cost      int64 `json:"cost"`
	}
	samples := []sample{}
	for _, m := range []int64{1, 2, 99, 100, 101, 10000} {
		for _, rate := range []int64{0, 1, 50, 333, 5000, 10000} {
			for _, mode := range []int{0, 1} {
				for _, minimum := range []buyerMinimum104{{}, {Threshold: 5, Cost: 2}, {Threshold: 10, Cost: 1}, {Threshold: 5, Cost: 200}} {
					samples = append(samples, sample{m, rate, mode, minimum.Threshold, minimum.Cost})
				}
			}
		}
	}
	body, e := json.Marshal(samples)
	if e != nil {
		t.Fatal(e)
	}
	runner := filepath.Join(t.TempDir(), "original-buyer-fee104.php")
	if e = os.WriteFile(runner, []byte(originalBuyerFee104PHP), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	out, e := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: reference, Target: "/reference/Pay.php"}, {Source: runner, Target: "/runner.php"}}, Args: []string{"/runner.php"}, Input: body})
	if e != nil {
		t.Fatal("actual original Release fee branch", e, string(out))
	}
	var got struct {
		SyntheticOnly bool                          `json:"synthetic_only"`
		Network       string                        `json:"network"`
		BranchSHA     string                        `json:"original_branch_sha256"`
		Results       []struct{ Charge, Net int64 } `json:"results"`
	}
	if e = json.Unmarshal(out, &got); e != nil || !got.SyntheticOnly || got.Network != "none" || len(got.Results) != len(samples) || len(got.BranchSHA) != 64 {
		t.Fatal(e, string(out))
	}
	for i, s := range samples {
		price, e := calculateBuyerChargeRelease104(s.Original, s.FeeBPS, s.Mode, nil, buyerMinimum104{Threshold: s.Threshold, Cost: s.Cost})
		if e != nil || price.Charge != got.Results[i].Charge || price.Net != got.Results[i].Net || price.Fee != got.Results[i].Charge-got.Results[i].Net {
			t.Fatal("original readable branch disagrees", s, price, got.Results[i], e)
		}
	}
	t.Logf("Actual pinned original Release fee branch %s matched %d zero/full-buyer and merchant/minimum cases; not encrypted Pro partial allocation proof", got.BranchSHA, len(samples))
}
