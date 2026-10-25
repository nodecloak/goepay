package payment

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTransferFailure76ClassificationPreservesReadableOriginalPayeeContract(t *testing.T) {
	raw, e := os.ReadFile("testdata/pro-transfer-payee-codes76.json")
	if e != nil {
		t.Fatal(e)
	}
	var original struct {
		SourceFragmentSHA256 string   `json:"source_fragment_sha256"`
		RawCodes             int      `json:"raw_codes"`
		UniqueCodes          int      `json:"unique_codes"`
		Codes                []string `json:"codes"`
	}
	if e = json.Unmarshal(raw, &original); e != nil || original.SourceFragmentSHA256 != "bde5e6a6304f55d87c6f150003b8db6fb63c5ce6c4d8511c0927e03bfa47634b" || original.RawCodes != 47 || original.UniqueCodes != 46 || len(original.Codes) != 46 {
		t.Fatal("invalid original contract fixture", original, e)
	}
	if len(originalPayeeFailureCodes) != original.UniqueCodes {
		t.Fatal("original allowlist expanded or lost a code")
	}
	for _, code := range original.Codes {
		for _, v := range []string{code, "40004:" + code} {
			if got := classifyTransferFailure76(v); got != transferFailurePayee76 {
				t.Fatal("original payee failure would close channel", v, got)
			}
		}
	}
	for _, code := range []string{"NOTENOUGH", "SYSTEM_ERROR", "NOAUTH", "40004:PAYER_BALANCE_NOT_ENOUGH", "40002:isv.invalid-signature", "40006:ACQ.ACCESS_FORBIDDEN"} {
		if got := classifyTransferFailure76(code); got != transferFailureChannel76 {
			t.Fatal("authenticated payer/channel failure not classified", code, got)
		}
	}
	for _, code := range []string{"", ":PAYEE_NOT_EXIST", "40004:", "4000:PAYEE_NOT_EXIST", "40004::PAYEE_NOT_EXIST", "10000:PAYEE_NOT_EXIST", "00000:SYSTEM_ERROR", "40004", "SYSTEM_ERROR\n", " SYSTEM_ERROR", "SYSTEM ERROR", "付款方错误"} {
		if got := classifyTransferFailure76(code); got != transferFailureIgnore76 {
			t.Fatal("non-canonical text became stop evidence", code, got)
		}
	}
}
