package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"slices"
	"time"
)

type ChannelSharingReceiverOperation struct {
	ID                   int64     `json:"id,string"`
	ReceiverID           int64     `json:"receiver_id,string"`
	Action               string    `json:"action"`
	ExternalNo           string    `json:"external_no"`
	OutNo                string    `json:"out_no"`
	State                string    `json:"state"`
	Source               string    `json:"source"`
	ReferenceOperationID int64     `json:"reference_operation_id,string"`
	RelationID           int64     `json:"-"`
	CreatedAt            time.Time `json:"created_at"`
}

const sharingReceiverOperationColumns = "id,receiver_id,action,external_no,out_no,state,created_at,source,COALESCE(reference_operation_id,0),relation_id"

func scanSharingReceiverOperation(row pgx.Row) (ChannelSharingReceiverOperation, error) {
	var op ChannelSharingReceiverOperation
	e := row.Scan(&op.ID, &op.ReceiverID, &op.Action, &op.ExternalNo, &op.OutNo, &op.State, &op.CreatedAt, &op.Source, &op.ReferenceOperationID, &op.RelationID)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return op, e
}
func (g ProviderFlow) sharingChannel(mid, channel int64) (adapter.ChannelSharingProvider, channelSharingBinding, error) {
	b := channelSharingBinding{MerchantID: mid, ChannelID: channel}
	if g.Store == nil || mid <= 0 || channel <= 0 {
		return nil, b, ErrInvalid
	}
	p := g.Providers[channel]
	sharing, ok := p.(adapter.ChannelSharingProvider)
	if !ok {
		return nil, b, adapter.ErrUnsupported
	}
	caps := sharing.ChannelSharingCapabilities()
	identity, ok := p.(adapter.ApplicationIdentity)
	if !ok || caps.Product == "" {
		return nil, b, adapter.ErrUnsupported
	}
	if _, ok = p.(adapter.TransferAccountIdentity); !ok {
		return nil, b, ErrConflict
	}
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return nil, b, e
	}
	b.Provider, b.AppID, b.Product, b.AccountScope = p.ID(), identity.ApplicationID(), caps.Product, scope
	relation, ok := p.(adapter.ChannelSharingRelationIdentity)
	if !ok || relation.ChannelSharingRelationAccount() == "" {
		return nil, b, adapter.ErrUnsupported
	}
	raw, _ := json.Marshal([]string{p.ID(), relation.ChannelSharingRelationAccount()})
	b.RelationScope = string(raw)
	if !validSharingBinding(b) {
		return nil, b, adapter.ErrUnsupported
	}
	return sharing, b, nil
}
func (s *Store) sharingReceiverBindingMatches(record sharingReceiverRecord, b channelSharingBinding) bool {
	hash, e := s.sharingDigest("account", []byte(b.AccountScope))
	physical, pe := s.sharingDigest("physical-relation-account", []byte(b.RelationScope))
	return e == nil && pe == nil && b.MerchantID == record.Binding.MerchantID && b.ChannelID == record.Binding.ChannelID && b.Provider == record.Binding.Provider && b.AppID == record.Binding.AppID && b.Product == record.Binding.Product && hmac.Equal(hash, record.AccountHash) && hmac.Equal(physical, record.RelationAccountHash)
}
func (g ProviderFlow) CreateChannelSharingReceiver(ctx context.Context, mid, channel int64, no string, input adapter.SharingReceiver, actor TransferBatchActor) (ChannelSharingReceiverView, error) {
	if !g.AllowLive {
		return ChannelSharingReceiverView{}, ErrLiveDisabled
	}
	p, b, e := g.sharingChannel(mid, channel)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	validator, ok := p.(adapter.ChannelSharingReceiverValidator)
	if !ok {
		return ChannelSharingReceiverView{}, adapter.ErrUnsupported
	}
	if e = validator.ValidateChannelSharingReceiver(input, false); e != nil {
		return ChannelSharingReceiverView{}, e
	}
	return g.Store.createChannelSharingReceiver(ctx, b, no, input, actor)
}

type sharingRelationRecord struct {
	ID, SourceReceiverID, LastOperationID, BoundOperationID int64
	State                                                   string
}

const sharingRelationColumns = "id,source_receiver_id,state,COALESCE(last_operation_id,0),COALESCE(bound_operation_id,0)"

func scanSharingRelation(row pgx.Row) (sharingRelationRecord, error) {
	var r sharingRelationRecord
	e := row.Scan(&r.ID, &r.SourceReceiverID, &r.State, &r.LastOperationID, &r.BoundOperationID)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return r, e
}

// Every relation mutation takes both identity locks, in the same order, before
// its registry and owner-local receiver locks. Draft creation takes neither.
func lockSharingRelationIdentity(ctx context.Context, tx pgx.Tx, r sharingReceiverRecord) error {
	keys := []string{hex.EncodeToString(r.RelationHash), hex.EncodeToString(r.RelationAuthHash)}
	slices.Sort(keys)
	for _, key := range slices.Compact(keys) {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,48))", "channel-sharing-physical-relation:"+key); e != nil {
			return e
		}
	}
	return nil
}
func sameSharingRelationConfiguration(a, b sharingReceiverRecord) bool {
	return a.Binding.Provider == b.Binding.Provider && a.Binding.AppID == b.Binding.AppID && a.Binding.Product == b.Binding.Product &&
		hmac.Equal(a.AccountHash, b.AccountHash) && hmac.Equal(a.RelationAccountHash, b.RelationAccountHash) &&
		hmac.Equal(a.RelationHash, b.RelationHash) && hmac.Equal(a.RelationAuthHash, b.RelationAuthHash)
}
func (s *Store) claimSharingReceiverOperation(ctx context.Context, mid, id int64, no, action string, b channelSharingBinding, actor TransferBatchActor) (ChannelSharingReceiverOperation, adapter.SharingReceiver, bool, error) {
	var input adapter.SharingReceiver
	var op ChannelSharingReceiverOperation
	if !actor.valid(mid) || !channelSharingExternalNo.MatchString(no) || action != "bind" && action != "unbind" {
		return op, input, false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return op, input, false, e
	}
	defer tx.Rollback(ctx)
	// Only immutable fields from this first read are used before locks.
	record, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2", mid, id))
	if e != nil {
		return op, input, false, e
	}
	if !s.sharingReceiverBindingMatches(record, b) {
		return op, input, false, ErrConflict
	}
	if e = lockSharingRelationIdentity(ctx, tx, record); e != nil {
		return op, input, false, e
	}
	global, e := scanSharingRelation(tx.QueryRow(ctx, "SELECT "+sharingRelationColumns+" FROM channel_sharing_relations WHERE relation_hash=$1 FOR UPDATE", record.RelationHash))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return op, input, false, e
	}
	missing := errors.Is(e, ErrNotFound)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", mid).Scan(&enabled); e != nil || !enabled {
		return op, input, false, ErrState
	}
	record, e = scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2 FOR UPDATE", mid, id))
	if e != nil {
		return op, input, false, e
	}
	input, e = s.openSharingReceiverRecord(record)
	if e != nil {
		return op, input, false, e
	}
	existing, e := scanSharingReceiverOperation(tx.QueryRow(ctx, "SELECT "+sharingReceiverOperationColumns+" FROM channel_sharing_receiver_operations WHERE receiver_id=$1 AND external_no=$2", id, no))
	if e == nil {
		if existing.Action != action {
			return op, input, false, ErrConflict
		}
		return existing, input, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return op, input, false, e
	}
	if action == "bind" && record.State != "draft" && record.State != "unbound" || action == "unbind" && record.State != "bound" {
		return op, input, false, ErrState
	}
	if action == "unbind" {
		var active bool
		if e = tx.QueryRow(ctx, "SELECT channel_sharing_receiver_has_references($1)", id).Scan(&active); e != nil {
			return op, input, false, e
		}
		if active {
			return op, input, false, ErrState
		}
	}
	var conflicting bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_relations WHERE relation_auth_hash=$1 AND relation_hash<>$2 AND state<>'unbound')", record.RelationAuthHash, record.RelationHash).Scan(&conflicting); e != nil {
		return op, input, false, e
	}
	if conflicting {
		return op, input, false, ErrConflict
	}
	if missing {
		if action != "bind" {
			return op, input, false, ErrState
		}
		global, e = scanSharingRelation(tx.QueryRow(ctx, "INSERT INTO channel_sharing_relations(relation_hash,relation_auth_hash,source_receiver_id) VALUES($1,$2,$3) RETURNING "+sharingRelationColumns, record.RelationHash, record.RelationAuthHash, id))
		if e != nil {
			return op, input, false, e
		}
	}
	source := "remote"
	var reference any
	if global.State == "bound" {
		original, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE id=$1", global.SourceReceiverID))
		if e != nil {
			return op, input, false, e
		}
		originalInput, e := s.openSharingReceiverRecord(original)
		if e != nil {
			return op, input, false, e
		}
		if !sameSharingRelationConfiguration(record, original) || originalInput != input {
			return op, input, false, ErrConflict
		}
		var leases int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_relation_leases WHERE relation_id=$1 AND released_by_operation_id IS NULL", global.ID).Scan(&leases); e != nil {
			return op, input, false, e
		}
		if leases < 1 || global.BoundOperationID == 0 {
			return op, input, false, ErrState
		}
		if action == "bind" || leases > 1 {
			source = "reference"
			reference = global.BoundOperationID
		}
		if action == "unbind" {
			var own bool
			if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_relation_leases WHERE relation_id=$1 AND receiver_id=$2 AND released_by_operation_id IS NULL)", global.ID, id).Scan(&own); e != nil {
				return op, input, false, e
			}
			if !own {
				return op, input, false, ErrState
			}
			if leases == 1 {
				var active bool
				if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_receivers WHERE relation_hash=$1 AND channel_sharing_receiver_has_references(id))", record.RelationHash).Scan(&active); e != nil {
					return op, input, false, e
				}
				if active {
					return op, input, false, ErrState
				}
			}
		}
	} else if global.State == "unbound" && action == "bind" {
		// A definite failed bind or confirmed final delete leaves no live
		// relationship. A fresh owner may claim it with its own original data.
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_relations SET source_receiver_id=$2,relation_auth_hash=$3 WHERE id=$1", global.ID, id, record.RelationAuthHash); e != nil {
			return op, input, false, e
		}
	} else {
		return op, input, false, ErrState
	}
	if e = requireFundsAction(ctx, tx, "channel-sharing-"+action, fmt.Sprintf("%d:%d:%s", mid, id, no)); e != nil {
		return op, input, false, e
	}
	raw, _ := json.Marshal([]any{id, action, record.AccountHash, record.InputHash, source, reference})
	hash, e := s.sharingDigest("receiver-operation", raw)
	if e != nil {
		return op, input, false, e
	}
	nonce := make([]byte, 15)
	if _, e = rand.Read(nonce); e != nil {
		return op, input, false, e
	}
	prefix := "PB"
	if action == "unbind" {
		prefix = "PU"
	}
	outNo := prefix + hex.EncodeToString(nonce)
	op, e = scanSharingReceiverOperation(tx.QueryRow(ctx, "INSERT INTO channel_sharing_receiver_operations(receiver_id,action,out_no,external_no,request_hash,actor_role,actor_id,actor_name,actor_class,relation_id,source,reference_operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING "+sharingReceiverOperationColumns, id, action, outNo, no, hash, actor.Role, actor.ID, actor.Name, actor.Class, global.ID, source, reference))
	if e != nil {
		return op, input, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state='claimed',claimed_at=clock_timestamp() WHERE id=$1", op.ID); e != nil {
		return op, input, false, e
	}
	next := "binding"
	if action == "unbind" {
		next = "unbinding"
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receivers SET state=$2,last_operation_id=$3 WHERE id=$1", id, next, op.ID); e != nil {
		return op, input, false, e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-receiver-operations", op.ID, action, "claimed"); e != nil {
		return op, input, false, e
	}
	if source == "remote" {
		if action == "bind" {
			_, e = tx.Exec(ctx, "UPDATE channel_sharing_relations SET state='binding',source_receiver_id=$2,relation_auth_hash=$3,last_operation_id=$4,bound_operation_id=NULL WHERE id=$1", global.ID, id, record.RelationAuthHash, op.ID)
		} else {
			_, e = tx.Exec(ctx, "UPDATE channel_sharing_relations SET state='unbinding',last_operation_id=$2 WHERE id=$1", global.ID, op.ID)
		}
		if e != nil {
			return op, input, false, e
		}
		op.State = "claimed"
	} else {
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state='succeeded' WHERE id=$1", op.ID); e != nil {
			return op, input, false, e
		}
		next = "bound"
		if action == "unbind" {
			next = "unbound"
		}
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receivers SET state=$2 WHERE id=$1", id, next); e != nil {
			return op, input, false, e
		}
		if action == "bind" {
			_, e = tx.Exec(ctx, "INSERT INTO channel_sharing_relation_leases(relation_id,receiver_id,bind_operation_id) VALUES($1,$2,$3)", global.ID, id, op.ID)
		} else {
			_, e = tx.Exec(ctx, "UPDATE channel_sharing_relation_leases SET released_by_operation_id=$3,released_at=statement_timestamp() WHERE relation_id=$1 AND receiver_id=$2 AND released_by_operation_id IS NULL", global.ID, id, op.ID)
		}
		if e != nil {
			return op, input, false, e
		}
		if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-receiver-operations", op.ID, action, "succeeded"); e != nil {
			return op, input, false, e
		}
		op.State = "succeeded"
	}
	if e = tx.Commit(ctx); e != nil {
		return op, input, false, e
	}
	return op, input, source == "remote", nil
}
func (s *Store) resolveSharingReceiverOperation(ctx context.Context, mid int64, op ChannelSharingReceiverOperation, state string, actor TransferBatchActor) error {
	if !actor.valid(mid) || state != "succeeded" && state != "failed" && state != "unknown" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	record, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2", mid, op.ReceiverID))
	if e != nil {
		return e
	}
	if e = lockSharingRelationIdentity(ctx, tx, record); e != nil {
		return e
	}
	global, e := scanSharingRelation(tx.QueryRow(ctx, "SELECT "+sharingRelationColumns+" FROM channel_sharing_relations WHERE id=$1 FOR UPDATE", op.RelationID))
	if e != nil {
		return e
	}
	var merchantID int64
	if e = tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR SHARE", mid).Scan(&merchantID); e != nil {
		return e
	}
	record, e = scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2 FOR UPDATE", mid, op.ReceiverID))
	if e != nil {
		return e
	}
	current, e := scanSharingReceiverOperation(tx.QueryRow(ctx, "SELECT "+sharingReceiverOperationColumns+" FROM channel_sharing_receiver_operations WHERE id=$1 AND receiver_id=$2 FOR UPDATE", op.ID, op.ReceiverID))
	if e != nil {
		return e
	}
	if current.State == state {
		return tx.Commit(ctx)
	}
	if current.State != "claimed" || current.Source != "remote" || record.LastOperation != op.ID || current.Action != op.Action || global.LastOperationID != op.ID {
		return ErrState
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state=$2 WHERE id=$1", op.ID, state); e != nil {
		return e
	}
	next := "unknown"
	globalNext := "unknown"
	if state == "succeeded" {
		next = "bound"
		globalNext = "bound"
		if op.Action == "unbind" {
			next = "unbound"
			globalNext = "unbound"
		}
	} else if state == "failed" {
		next = "draft"
		globalNext = "unbound"
		if op.Action == "unbind" {
			next = "bound"
			globalNext = "bound"
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receivers SET state=$2 WHERE id=$1", op.ReceiverID, next); e != nil {
		return e
	}
	var bound any
	if global.BoundOperationID > 0 {
		bound = global.BoundOperationID
	}
	if state == "succeeded" && op.Action == "bind" {
		bound = op.ID
		_, e = tx.Exec(ctx, "INSERT INTO channel_sharing_relation_leases(relation_id,receiver_id,bind_operation_id) VALUES($1,$2,$3)", global.ID, op.ReceiverID, op.ID)
	} else if state == "succeeded" && op.Action == "unbind" {
		_, e = tx.Exec(ctx, "UPDATE channel_sharing_relation_leases SET released_by_operation_id=$3,released_at=statement_timestamp() WHERE relation_id=$1 AND receiver_id=$2 AND released_by_operation_id IS NULL", global.ID, op.ReceiverID, op.ID)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_relations SET state=$2,bound_operation_id=$3 WHERE id=$1", global.ID, globalNext, bound); e != nil {
		return e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-receiver-operations", op.ID, op.Action, state); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) receiverBeforeIO(ctx context.Context, mid, id int64, expected channelSharingBinding) (adapter.ChannelSharingProvider, error) {
	p, b, e := g.sharingChannel(mid, expected.ChannelID)
	if e != nil {
		return nil, e
	}
	record, e := scanSharingReceiver(g.Store.pool.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2", mid, id))
	if e != nil {
		return nil, e
	}
	if !g.Store.sharingReceiverBindingMatches(record, b) {
		return nil, ErrConflict
	}
	var enabled bool
	if e = g.Store.pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", mid).Scan(&enabled); e != nil || !enabled {
		return nil, ErrState
	}
	return p, nil
}
func (g ProviderFlow) receiverOperation(ctx context.Context, mid, id int64, no, action string, actor TransferBatchActor) (ChannelSharingReceiverView, error) {
	if !g.AllowLive {
		return ChannelSharingReceiverView{}, ErrLiveDisabled
	}
	if g.Store == nil || !actor.valid(mid) {
		return ChannelSharingReceiverView{}, ErrInvalid
	}
	record, e := scanSharingReceiver(g.Store.pool.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2", mid, id))
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	p, b, e := g.sharingChannel(mid, record.Binding.ChannelID)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	if !g.Store.sharingReceiverBindingMatches(record, b) {
		return ChannelSharingReceiverView{}, ErrConflict
	}
	input, e := g.Store.openSharingReceiverRecord(record)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	validator, ok := p.(adapter.ChannelSharingReceiverValidator)
	if !ok {
		return ChannelSharingReceiverView{}, adapter.ErrUnsupported
	}
	if e = validator.ValidateChannelSharingReceiver(input, action == "unbind"); e != nil {
		return ChannelSharingReceiverView{}, e
	}
	op, input, owner, e := g.Store.claimSharingReceiverOperation(ctx, mid, id, no, action, b, actor)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	if !owner {
		view, read := g.Store.ChannelSharingReceiver(ctx, mid, id, actor)
		if read != nil {
			return view, read
		}
		switch op.State {
		case "succeeded":
			return view, nil
		case "failed":
			return view, ErrState
		default:
			return view, &adapter.UnknownOutcome{Operation: "receiver-" + action, Cause: ErrState}
		}
	}
	p, e = g.receiverBeforeIO(ctx, mid, id, b)
	if e != nil {
		save := g.Store.resolveSharingReceiverOperation(ctx, mid, op, "failed", actor)
		if save != nil {
			return ChannelSharingReceiverView{}, errors.Join(e, save)
		}
		view, _ := g.Store.ChannelSharingReceiver(ctx, mid, id, actor)
		return view, e
	}
	if action == "bind" {
		e = p.BindChannelSharingReceiver(ctx, op.OutNo, input)
	} else {
		e = p.UnbindChannelSharingReceiver(ctx, op.OutNo, input)
	}
	state := "succeeded"
	if e != nil {
		state = "unknown"
		var rejected *adapter.ProviderError
		if errors.As(e, &rejected) && (definiteProviderRejection(rejected) || rejected.Code == "SHARING_RELATION_FAIL" || rejected.Code == "NO_AUTH" || rejected.Code == "LACK_PARAMS" || rejected.Code == "APPID_MCHID_NOT_MATCH") {
			state = "failed"
		}
		if state == "unknown" {
			var unknown *adapter.UnknownOutcome
			if !errors.As(e, &unknown) {
				e = &adapter.UnknownOutcome{Operation: "receiver-" + action, Cause: e}
			}
		}
	}
	if save := g.Store.resolveSharingReceiverOperation(ctx, mid, op, state, actor); save != nil {
		return ChannelSharingReceiverView{}, &adapter.UnknownOutcome{Operation: "persist-receiver-" + action, Cause: errors.Join(e, save)}
	}
	view, read := g.Store.ChannelSharingReceiver(ctx, mid, id, actor)
	if read != nil {
		return view, read
	}
	return view, e
}
func (g ProviderFlow) BindChannelSharingReceiver(ctx context.Context, mid, id int64, no string, actor TransferBatchActor) (ChannelSharingReceiverView, error) {
	return g.receiverOperation(ctx, mid, id, no, "bind", actor)
}
func (g ProviderFlow) UnbindChannelSharingReceiver(ctx context.Context, mid, id int64, no string, actor TransferBatchActor) (ChannelSharingReceiverView, error) {
	return g.receiverOperation(ctx, mid, id, no, "unbind", actor)
}
