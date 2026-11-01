package payment

import (
	"encoding/json"
	"sort"
	"strconv"

	adapter "goepay/internal/adapters/payment"
)

// Dependencies come only from AEAD-authenticated, granted parent accounts that
// this request already restored. User supplied AppIDs cannot select a vault row.
// Unknown provenance is an error, never an assumption of no managed binding.
func (g ProviderFlow) ManagedWeixinApplicationDependencies(payType string) ([]int64, error) {
	seen := map[int64]bool{}
	ids := []int64{}
	for _, b := range g.parentBindings {
		if b == nil || b.account.kind != "wxpay" || payType != "" && payType != "wxpay" {
			continue
		}
		if b.legacyChildAuthority {
			continue
		}
		raw := b.bootstrapRaw
		if len(b.account.ciphertext) > 0 {
			var e error
			raw, e = g.Store.openParentAccount(b.account)
			if e != nil {
				return nil, e
			}
		}
		if len(raw) == 0 {
			return nil, ErrConflict
		}
		var payload struct {
			Bindings struct {
				Web, Mini *struct {
					ID     string `json:"id"`
					Origin string `json:"managed_vault_fingerprint"`
				}
			} `json:"weixin_bindings"`
		}
		if json.Unmarshal(raw, &payload) != nil {
			return nil, ErrConflict
		}
		for _, p := range []*struct {
			ID     string `json:"id"`
			Origin string `json:"managed_vault_fingerprint"`
		}{payload.Bindings.Web, payload.Bindings.Mini} {
			if p == nil || p.Origin == "" {
				continue
			}
			if !adapter.ValidManagedWeixinVaultFingerprint(p.Origin) {
				return nil, ErrConflict
			}
			id, e := strconv.ParseInt(p.ID, 10, 64)
			if e != nil || id <= 0 || strconv.FormatInt(id, 10) != p.ID {
				return nil, ErrConflict
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
