package kyc

import (
	"bytes"
	"context"
	"encoding/json"
	domain "goepay/internal/kyc"
	"io"
	"net/http"
	"testing"
)

func TestOCR233ProviderSpecificRawImageLimit(t *testing.T) {
	calls := 0
	p := peerAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 3<<20 {
			t.Error("raw image not preserved", len(body), err)
		}
		data, _ := json.Marshal(map[string]any{"data": map[string]any{"bankName": "Synthetic Bank", "cardNumber": "synthetic-card"}})
		json.NewEncoder(w).Encode(map[string]any{"Data": string(data)})
	})
	snap := domain.Snapshot{OCRType: "aliyun", Credentials: domain.Credentials{OCRAliyunID: "synthetic-id", OCRAliyunKey: "synthetic-key"}}
	if _, err := p.Recognize(context.Background(), snap, "bank_card", bytes.Repeat([]byte{0}, 3<<20)); err != nil {
		t.Fatal("valid Aliyun payload rejected by Baidu limit", err)
	}
	if calls != 1 {
		t.Fatal("expected exactly one recognition", calls)
	}
	if _, err := p.Recognize(context.Background(), snap, "bank_card", make([]byte, (4<<20)+1)); err == nil {
		t.Fatal("Aliyun max image bound bypassed")
	}
	snap.OCRType = "baidu"
	if _, err := p.Recognize(context.Background(), snap, "bank_card", make([]byte, (2<<20)+1)); err == nil {
		t.Fatal("Baidu max image bound bypassed")
	}
	if calls != 1 {
		t.Fatal("oversized payload reached provider", calls)
	}
}
