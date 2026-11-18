package server

import (
	"encoding/json"
	"strings"
	"testing"

	"goepay/internal/cryptopay"
)

func TestCryptoChannelInputUsesGroupAssignmentWithoutMerchantGrantList(t *testing.T) {
	create := `{"name":"USDT group assigned","network":"tron","enabled":false,"archived":false,"fee_bps":75,"min_amount":"0","max_amount":"100","day_limit":"200","expected_revision":"0","request_id":"1234567890abcdef1234567890abcdef"}`
	in, err := decodeCryptoChannelInput([]byte(create), 0)
	if err != nil || in.Name != "USDT group assigned" || in.FeeBPS != 75 || in.ExpectedRevision != 0 || in.Enabled {
		t.Fatalf("channel definition must not require an inverse merchant grant list: input=%+v err=%v", in, err)
	}
	update := strings.Replace(strings.Replace(create, `,"request_id":"1234567890abcdef1234567890abcdef"`, "", 1), `"expected_revision":"0"`, `"expected_revision":"7"`, 1)
	if in, err = decodeCryptoChannelInput([]byte(update), 31); err != nil || in.ID != 31 || in.ExpectedRevision != 7 {
		t.Fatalf("ordinary channel update requires group-independent fields only: input=%+v err=%v", in, err)
	}
	for _, field := range []string{`"merchant_ids":[]`, `"merchant_ids":["1"]`, `"merchant_ids":null`, `"enabled":null`, `"expected_revision":null`} {
		raw := strings.TrimSuffix(create, "}") + "," + field + "}"
		if _, err := decodeCryptoChannelInput([]byte(raw), 0); err == nil {
			t.Fatalf("unknown or ambiguous channel authorization field accepted: %s", field)
		}
	}
	if _, err := decodeCryptoChannelInput([]byte(strings.Replace(create, `"fee_bps":75`, `"fee_bps":null`, 1)), 0); err == nil {
		t.Fatalf("null fee must not clear the existing fee: %v", err)
	}
	encoded, err := json.Marshal(cryptoChannelViewOf(cryptopay.Channel{ID: 31, Network: "tron"}, nil))
	if err != nil || strings.Contains(string(encoded), "merchant_ids") {
		t.Fatalf("channel DTO still exposes the removed inverse assignment list: %s, %v", encoded, err)
	}
}
