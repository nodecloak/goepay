package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
)

type sharingObservationProof struct {
	ProviderNo, State string
	Items             []ChannelSharingOrderItem
}

// Database constraints enforce the ledger shape. The keyed fact also proves
// that the application previously authenticated the corresponding provider
// result, before a later read may authorize a release from that fact.
func (s *Store) verifySharingObservations(ctx context.Context, tx referralReader, r sharingOrderRecord) error {
	if len(r.Items) != len(r.Payment.Items) || len(r.Items) == 0 {
		return ErrConflict
	}
	rows, e := tx.Query(ctx, "SELECT receiver_id,COALESCE(last_observation_id,0) FROM channel_sharing_order_items WHERE sharing_id=$1 ORDER BY position", r.ID)
	if e != nil {
		return e
	}
	refs := make([]int64, 0, len(r.Items))
	for rows.Next() {
		var receiver, id int64
		if e = rows.Scan(&receiver, &id); e != nil {
			rows.Close()
			return e
		}
		if len(refs) >= len(r.Items) || receiver != r.Items[len(refs)].ReceiverID {
			rows.Close()
			return ErrConflict
		}
		refs = append(refs, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(refs) != len(r.Items) {
		return ErrConflict
	}
	proofs := map[int64]sharingObservationProof{}
	ids := append(append([]int64{}, refs...), r.LastObservationID)
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := proofs[id]; ok {
			continue
		}
		var p sharingObservationProof
		var owner int64
		var raw, hash []byte
		if e = tx.QueryRow(ctx, "SELECT sharing_id,provider_order_no,state,items,fingerprint FROM channel_sharing_observations WHERE id=$1", id).Scan(&owner, &p.ProviderNo, &p.State, &raw, &hash); e != nil {
			return e
		}
		if owner != r.ID || p.ProviderNo != r.ProviderOrderNo || json.Unmarshal(raw, &p.Items) != nil || len(p.Items) != len(r.Items) {
			return ErrConflict
		}
		success, failed := 0, 0
		for i, item := range p.Items {
			if item.ReceiverID != r.Payment.Items[i].ReceiverID || item.Amount != r.Payment.Items[i].Amount || !sharingSafeField(item.ProviderDetailNo, 64, false) {
				return ErrConflict
			}
			switch item.State {
			case "succeeded":
				if item.ProviderDetailNo == "" || item.FailureCode != "" {
					return ErrConflict
				}
				success++
			case "failed":
				if item.FailureCode != sharingFailureCode(item.State, item.FailureCode) {
					return ErrConflict
				}
				failed++
			case "pending":
				if item.FailureCode != "" {
					return ErrConflict
				}
			default:
				return ErrConflict
			}
		}
		if p.State != "pending" {
			expected := "partial"
			if success == len(p.Items) {
				expected = "succeeded"
			}
			if failed == len(p.Items) {
				expected = "failed"
			}
			if success+failed != len(p.Items) || p.State != expected {
				return ErrConflict
			}
		}
		fact, e := json.Marshal([]any{r.ID, r.OutNo, r.ProviderTradeNo, p.ProviderNo, p.State, p.Items})
		if e != nil {
			return e
		}
		actual, e := s.sharingDigest("sharing-observation", fact)
		if e != nil {
			return e
		}
		if !hmac.Equal(actual, hash) {
			return ErrConflict
		}
		proofs[id] = p
	}
	if r.LastObservationID == 0 {
		if r.ProviderOrderNo != "" || r.Completed || r.State != "claimed" && r.State != "unknown" && r.State != "failed" {
			return ErrConflict
		}
	} else {
		last := proofs[r.LastObservationID]
		if last.State != r.State || r.Completed != (last.State != "pending") {
			return ErrConflict
		}
	}
	for i, item := range r.Items {
		if item.ReceiverID != r.Payment.Items[i].ReceiverID || item.Amount != r.Payment.Items[i].Amount {
			return ErrConflict
		}
		if refs[i] == 0 {
			if item.State != "pending" || item.ProviderDetailNo != "" || item.FailureCode != "" {
				return ErrConflict
			}
		} else if proofs[refs[i]].Items[i] != item {
			return ErrConflict
		}
	}
	return nil
}
