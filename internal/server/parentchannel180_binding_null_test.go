package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestParentChannel180WeixinBatchNullTypeRejectedBeforeAnyOriginalWrite(t *testing.T) {
	setup, local, _, _, state := binding180Ready(t)
	f := setup.f
	body, err := json.Marshal(map[string]any{"type": 0, "weixin": "81", "items": []any{map[string]any{"id": setup.id, "expected_fingerprint": state["fingerprint"], "expected_bindings_fingerprint": local.ParentWeixinCatalog().Fingerprint}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, table := range []string{"runtime_parent_channels", "runtime_parent_channel_accounts", "runtime_parent_channel_grants", "runtime_parent_channel_requests", "backoffice_audit"} {
			var digest string
			// Identifiers are a closed test-owned list, never request parameters.
			query := fmt.Sprintf(`SELECT COALESCE(md5(string_agg(to_jsonb(t)::text,'' ORDER BY to_jsonb(t)::text)),'empty') FROM %s t`, table)
			if err := f.p.QueryRow(t.Context(), query).Scan(&digest); err != nil {
				t.Fatal(err)
			}
			out[table] = digest
		}
		var outVersion string
		if err := f.p.QueryRow(t.Context(), `SELECT account_version::text FROM runtime_parent_channels WHERE id=$1`, setup.id).Scan(&outVersion); err != nil {
			t.Fatal(err)
		}
		out["original_account_version"] = outVersion
		return out
	}
	for _, literal := range []string{"null", " \n null \t "} {
		before := snapshot()
		invalid := bytes.Replace(body, []byte(`"type":0`), []byte(`"type":`+literal), 1)
		if bytes.Equal(invalid, body) {
			t.Fatal("test fixture did not replace the exact type")
		}
		result := parent180HTTP(f, "POST", "/bind-weixin", string(invalid))
		if result.Code != 400 {
			t.Fatal("null type reached the integer default branch", result.Code)
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatal("null selector changed original account version, registry or audit")
		}
	}
	if result := parent180HTTP(f, "POST", "/bind-weixin", string(body)); result.Code != 200 {
		t.Fatal("otherwise identical non-null original selection is not valid", result.Code, result.Body.String())
	}
	if setup.creates.Load() != 0 {
		t.Fatal("configuration boundary check sent a payment")
	}
}
