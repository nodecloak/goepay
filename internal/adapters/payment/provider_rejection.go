package payment

import "errors"

// ProviderRejectionReceipt is transport provenance, not a new sending right or
// permission to release a reserve. Callers still match provider, operation and
// the product's explicit definitive-rejection vocabulary.
type ProviderRejectionReceipt struct {
	Provider       string `json:"provider"`
	Operation      string `json:"operation"`
	Code           string `json:"code"`
	Authentication string `json:"authentication"`
	ResponseSHA256 string `json:"response_sha256"`
}

func VerifiedProviderRejection(err error) (ProviderRejectionReceipt, bool) {
	var rejected *ProviderError
	if err == nil || uncertainProviderOutcome(err) || !errors.As(err, &rejected) || rejected.rejection == nil {
		return ProviderRejectionReceipt{}, false
	}
	v := rejected.rejection
	if !v.verifiedTLS || rejected.Code != v.code || v.operation == "" || len(v.responseSHA256) != 64 {
		return ProviderRejectionReceipt{}, false
	}
	if v.provider == "alipay" {
		if v.authentication != "alipay_rsa2" {
			return ProviderRejectionReceipt{}, false
		}
	} else if v.provider == "wxpay" {
		if v.authentication != "wechat_v2_md5" && v.authentication != "wechat_v2_mtls" {
			return ProviderRejectionReceipt{}, false
		}
	} else {
		return ProviderRejectionReceipt{}, false
	}
	return ProviderRejectionReceipt{Provider: v.provider, Operation: v.operation, Code: v.code, Authentication: v.authentication, ResponseSHA256: v.responseSHA256}, true
}
