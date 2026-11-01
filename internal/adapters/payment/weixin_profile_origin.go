package payment

import (
	"encoding/hex"
	"strings"
)

const ManagedWeixinVaultOrigin = "vault186:"

func ValidManagedWeixinVaultFingerprint(value string) bool {
	if !strings.HasPrefix(value, ManagedWeixinVaultOrigin) {
		return false
	}
	text := strings.TrimPrefix(value, ManagedWeixinVaultOrigin)
	raw, e := hex.DecodeString(text)
	return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == text
}
