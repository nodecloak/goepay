package cryptochain

import (
	"strings"
	"testing"
)

func TestAddressCanonicalizationAndChecksum(t *testing.T) {
	for _, tc := range []struct {
		chain       Chain
		input, want string
	}{
		{Tron, "41a614f803b6fd780986a42c78ec9c7f77e6ded13c", TronUSDTContract},
		{Tron, TronUSDTContract, TronUSDTContract},
		{Ethereum, "0xdAC17F958D2ee523a2206206994597C13D831ec7", EthereumUSDTContract},
	} {
		got, err := NormalizeAddress(tc.chain, tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizeAddress(%s)=%q,%v, want %q", tc.input, got, err, tc.want)
		}
	}
	for _, tc := range []struct {
		chain Chain
		input string
	}{
		{Tron, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6u"},
		{Tron, EthereumUSDTContract}, {Ethereum, TronUSDTContract},
		{Tron, "42a614f803b6fd780986a42c78ec9c7f77e6ded13c"},
		{Ethereum, "0xdAC17F958D2ee523a2206206994597C13D831ec8"},
		{Ethereum, "0x" + strings.Repeat("0", 40)},
	} {
		if _, err := NormalizeAddress(tc.chain, tc.input); err == nil {
			t.Fatalf("accepted invalid %s address %s", tc.chain, tc.input)
		}
	}
}

func TestAtomicAmountRejectsLossyOrAmbiguousInput(t *testing.T) {
	for _, s := range []string{"1", "1000001", "340282366920938463463374607431768211456"} {
		v, err := ParseAtomicAmount(s)
		if err != nil || v.String() != s {
			t.Fatalf("%s: %v %v", s, v, err)
		}
	}
	for _, s := range []string{"", "0", "00", "01", "-1", "1.0", "1e6", "+1", " 1", "1 ", "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		if _, err := ParseAtomicAmount(s); err == nil {
			t.Fatalf("accepted invalid amount %q", s)
		}
	}
}
