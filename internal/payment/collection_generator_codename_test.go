package payment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// F10: the merchant's default aggregate-code name (account/onecodeInfo +
// account/editCodename) is stored on the profile, falls back to the display
// name, can be set and cleared, and rejects out-of-bounds names.
func TestMerchantOnecodeNameDefaultsSetsAndClears(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 101, Name: "示例商户", APIKey: "test-secret-long-enough"}); e != nil {
		t.Fatal(e)
	}

	// No stored default yet: falls back to the display name, stored=false.
	name, stored, e := s.MerchantOnecodeName(ctx, 101)
	if e != nil {
		t.Fatal(e)
	}
	if name != "示例商户" || stored {
		t.Fatalf("default = %q stored=%v, want display name, not stored", name, stored)
	}

	// Set a persistent default.
	got, e := s.SetMerchantOnecodeName(ctx, 101, "我的收款")
	if e != nil || got != "我的收款" {
		t.Fatalf("set codename: %q %v", got, e)
	}
	name, stored, e = s.MerchantOnecodeName(ctx, 101)
	if e != nil || name != "我的收款" || !stored {
		t.Fatalf("read after set: %q stored=%v err=%v", name, stored, e)
	}

	// Clearing it falls back to the display name again.
	got, e = s.SetMerchantOnecodeName(ctx, 101, "")
	if e != nil || got != "示例商户" {
		t.Fatalf("clear codename: %q %v", got, e)
	}
	_, stored, e = s.MerchantOnecodeName(ctx, 101)
	if e != nil || stored {
		t.Fatalf("read after clear: stored=%v err=%v", stored, e)
	}

	// Out-of-bounds names are rejected (over 100 runes, control chars).
	if _, e = s.SetMerchantOnecodeName(ctx, 101, strings.Repeat("名", 101)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("over-long name not rejected: %v", e)
	}
	if _, e = s.SetMerchantOnecodeName(ctx, 101, "bad\x00name"); !errors.Is(e, ErrInvalid) {
		t.Fatalf("control-char name not rejected: %v", e)
	}

	// An unknown merchant is a state error, never a silent empty default.
	if _, _, e = s.MerchantOnecodeName(ctx, 999); !errors.Is(e, ErrState) {
		t.Fatalf("unknown merchant: %v", e)
	}
}
