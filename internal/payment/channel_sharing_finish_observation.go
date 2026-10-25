package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func sharingFinishActorValid(actor TransferBatchActor, mid int64) bool {
	return actor.valid(mid) || actor.Role == "system" && actor.ID == 0 && actor.Name == "channel-sharing-finish-worker" && actor.Class == ""
}
func (s *Store) sharingFinishObservationFingerprint(r sharingFinishRecord, result adapter.ChannelSharingFinishResult, source string) ([]byte, error) {
	raw, e := json.Marshal([]any{r.ID, hex.EncodeToString(r.RequestHash), r.OutNo, r.ProviderTradeNo, result.ProviderOrderNo, result.State, result.UnsplitAmount, source})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("sharing-finish-observation", raw)
}
func validSharingFinishResult(r sharingFinishRecord, v adapter.ChannelSharingFinishResult, source string) error {
	if source != "submit" && source != "query" || v.OrderNo != r.OutNo || v.ProviderTradeNo != r.ProviderTradeNo || !sharingSafeField(v.ProviderOrderNo, 64, true) || r.ProviderOrderNo != "" && v.ProviderOrderNo != r.ProviderOrderNo {
		return adapter.ErrAuthentication
	}
	if v.State != "pending" && v.State != "succeeded" && v.State != "failed" || source == "submit" && v.State != "pending" || v.State == "succeeded" && v.UnsplitAmount != r.ExpectedUnsplit || v.State != "succeeded" && v.UnsplitAmount != 0 {
		return adapter.ErrAuthentication
	}
	return nil
}
func (s *Store) verifySharingFinishObservations(ctx context.Context, tx referralReader, r sharingFinishRecord) error {
	rows, e := tx.Query(ctx, "SELECT id,fingerprint,source,provider_order_no,state,unsplit_amount FROM channel_sharing_finish_observations WHERE finish_id=$1 ORDER BY id", r.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	var last, lastSeen int64
	var final *adapter.ChannelSharingFinishResult
	for rows.Next() {
		var id int64
		var hash []byte
		var source string
		v := adapter.ChannelSharingFinishResult{OrderNo: r.OutNo, ProviderTradeNo: r.ProviderTradeNo}
		if e = rows.Scan(&id, &hash, &source, &v.ProviderOrderNo, &v.State, &v.UnsplitAmount); e != nil {
			return e
		}
		if e = validSharingFinishResult(r, v, source); e != nil {
			return e
		}
		h, e := s.sharingFinishObservationFingerprint(r, v, source)
		if e != nil || !hmac.Equal(hash, h) {
			return ErrConflict
		}
		if final != nil && (v.State != final.State || v.ProviderOrderNo != final.ProviderOrderNo || v.UnsplitAmount != final.UnsplitAmount) {
			return ErrConflict
		}
		if v.State != "pending" {
			copy := v
			final = &copy
		}
		lastSeen = id
		if id == r.LastObservationID {
			if v.State != r.State || v.ProviderOrderNo != r.ProviderOrderNo {
				return ErrConflict
			}
			last = id
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if r.LastObservationID != last || r.LastObservationID != lastSeen {
		return ErrConflict
	}
	return nil
}
func (s *Store) applySharingFinishObservation(ctx context.Context, original sharingFinishRecord, result adapter.ChannelSharingFinishResult, source string, actor TransferBatchActor) error {
	if !sharingFinishActorValid(actor, original.MerchantID) {
		return ErrInvalid
	}
	if e := validSharingFinishResult(original, result, source); e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", original.OrderID); e != nil {
		return e
	}
	r, e := s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.id=$1 FOR UPDATE OF p", original.ID))
	if e != nil {
		return e
	}
	if r.State == "succeeded" || r.State == "failed" {
		return tx.Commit(ctx)
	}
	if e = validSharingFinishResult(r, result, source); e != nil {
		return e
	}
	hash, e := s.sharingFinishObservationFingerprint(r, result, source)
	if e != nil {
		return e
	}
	var observation int64
	e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_finish_observations(finish_id,fingerprint,source,provider_order_no,state,unsplit_amount,actor_role,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(finish_id,fingerprint) DO NOTHING RETURNING id`, r.ID, hash, source, result.ProviderOrderNo, result.State, result.UnsplitAmount, actor.Role, actor.ID, actor.Name).Scan(&observation)
	if errors.Is(e, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	var journal any
	if result.State == "succeeded" {
		var available int64
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", r.MerchantID).Scan(&available); e != nil {
			return e
		}
		key := fmt.Sprintf("channel-sharing-finish:%d:release", r.ID)
		if e = postByAccountIDs(ctx, tx, key, "verified original channel sharing finish", r.FundingAccount, available, r.ReleaseAmount); e != nil {
			return e
		}
		var id int64
		if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&id); e != nil {
			return e
		}
		journal = id
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state=$2,provider_order_no=$3,last_observation_id=$4,release_journal_id=$5,failure_proof=NULL WHERE id=$1", r.ID, result.State, result.ProviderOrderNo, observation, journal); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-finishes", r.ID, source+"-result", result.State); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
