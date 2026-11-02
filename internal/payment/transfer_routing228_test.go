package payment

import "testing"

func TestTransferRouting228ExplicitVersionedValues(t *testing.T) {
	for _, raw := range []string{"inherit", "disabled", "manual", "channel:1", "channel:9007199254740993", "channel:9223372036854775807"} {
		route, err := ParseTransferRoute228(raw)
		if err != nil || route.String() != raw {
			t.Fatalf("versioned route did not round trip: %q %v", raw, err)
		}
	}
	// Legacy 1 is an enable flag, never implicit authority to select channel 1.
	for _, raw := range []string{"", "0", "1", "-1", "channel:0", "channel:-1", "channel:01", "channel:9223372036854775808", "channel:1 ", "CHANNEL:1"} {
		if _, err := ParseTransferRoute228(raw); err == nil {
			t.Fatalf("ambiguous or invalid route accepted: %q", raw)
		}
	}
}
