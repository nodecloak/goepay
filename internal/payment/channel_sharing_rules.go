package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type ChannelSharingRuleInput struct {
	Enabled       bool                     `json:"enabled"`
	MinimumAmount int64                    `json:"minimum_amount,string"`
	Methods       []string                 `json:"methods"`
	Items         []ChannelSharingRuleItem `json:"items"`
}
type ChannelSharingRuleView struct {
	ID         int64  `json:"id,string"`
	MerchantID int64  `json:"merchant_id,string"`
	ChannelID  int64  `json:"channel_id,string"`
	Revision   int64  `json:"revision,string"`
	Provider   string `json:"provider"`
	AppID      string `json:"app_id"`
	Product    string `json:"product"`
	ChannelSharingRuleInput
	RefundMode string    `json:"refund_mode"`
	AutoFinish bool      `json:"auto_finish"`
	CreatedAt  time.Time `json:"created_at"`
}
type sharingRuleRecord struct {
	ChannelSharingRuleView
	AccountHash, InputHash []byte
}

func (r sharingRuleRecord) binding() channelSharingBinding {
	return channelSharingBinding{MerchantID: r.MerchantID, ChannelID: r.ChannelID, Provider: r.Provider, AppID: r.AppID, Product: r.Product}
}

var sharingMethodPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func normalizeSharingRule(input ChannelSharingRuleInput) (ChannelSharingRuleInput, error) {
	if input.MinimumAmount < 0 || input.MinimumAmount > maxChannelSharingMoney || len(input.Methods) < 1 || len(input.Methods) > 10 || validateChannelSharingRules(input.Items) != nil {
		return input, ErrInvalid
	}
	input.Methods = slices.Clone(input.Methods)
	input.Items = slices.Clone(input.Items)
	slices.Sort(input.Methods)
	for i, method := range input.Methods {
		if !sharingMethodPattern.MatchString(method) || i > 0 && input.Methods[i-1] == method {
			return input, ErrInvalid
		}
	}
	return input, nil
}

const sharingRuleColumns = "id,merchant_id,channel_id,revision,provider,app_id,product,account_hash,input_hash,enabled,minimum_amount,methods,refund_mode,auto_finish,created_at"

func scanSharingRule(ctx context.Context, tx pgx.Tx, row pgx.Row) (sharingRuleRecord, error) {
	var r sharingRuleRecord
	var methods []byte
	e := row.Scan(&r.ID, &r.MerchantID, &r.ChannelID, &r.Revision, &r.Provider, &r.AppID, &r.Product, &r.AccountHash, &r.InputHash, &r.Enabled, &r.MinimumAmount, &methods, &r.RefundMode, &r.AutoFinish, &r.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if json.Unmarshal(methods, &r.Methods) != nil {
		return r, ErrInvalid
	}
	rows, e := tx.Query(ctx, "SELECT receiver_id,mode,rate_bps,fixed_amount FROM channel_sharing_rule_items WHERE rule_id=$1 ORDER BY position", r.ID)
	if e != nil {
		return r, e
	}
	defer rows.Close()
	for rows.Next() {
		var item ChannelSharingRuleItem
		if e = rows.Scan(&item.ReceiverID, &item.Mode, &item.RateBPS, &item.FixedAmount); e != nil {
			return r, e
		}
		r.Items = append(r.Items, item)
	}
	if e = rows.Err(); e != nil {
		return r, e
	}
	if _, e = normalizeSharingRule(r.ChannelSharingRuleInput); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Store) sharingRuleFingerprint(b channelSharingBinding, account []byte, input ChannelSharingRuleInput) ([]byte, error) {
	if !validSharingPublicBinding(b) || len(account) != 32 {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal([]any{b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Product, hex.EncodeToString(account), "merchant_funded", true, input})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("rule-input", raw)
}
func (s *Store) verifySharingRule(r sharingRuleRecord) error {
	hash, e := s.sharingRuleFingerprint(r.binding(), r.AccountHash, r.ChannelSharingRuleInput)
	if e != nil || !hmac.Equal(hash, r.InputHash) {
		return ErrConflict
	}
	return nil
}
func (s *Store) readChannelSharingRule(ctx context.Context, mid, id int64, current bool, actor TransferBatchActor) (ChannelSharingRuleView, error) {
	if !actor.valid(mid) || id <= 0 {
		return ChannelSharingRuleView{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	defer tx.Rollback(ctx)
	where := "merchant_id=$1 AND id=$2"
	if current {
		where = "merchant_id=$1 AND id=(SELECT rule_id FROM channel_sharing_rule_current WHERE merchant_id=$1 AND channel_id=$2)"
	}
	r, e := scanSharingRule(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingRuleColumns+" FROM channel_sharing_rules WHERE "+where, mid, id))
	if e != nil {
		return r.ChannelSharingRuleView, e
	}
	if e = s.verifySharingRule(r); e != nil {
		return ChannelSharingRuleView{}, e
	}
	return r.ChannelSharingRuleView, tx.Commit(ctx)
}
func (s *Store) ChannelSharingRule(ctx context.Context, mid, channel int64, actor TransferBatchActor) (ChannelSharingRuleView, error) {
	return s.readChannelSharingRule(ctx, mid, channel, true, actor)
}
func (s *Store) ChannelSharingRuleVersion(ctx context.Context, mid, id int64, actor TransferBatchActor) (ChannelSharingRuleView, error) {
	return s.readChannelSharingRule(ctx, mid, id, false, actor)
}

func (g ProviderFlow) SetChannelSharingRule(ctx context.Context, mid, channel int64, input ChannelSharingRuleInput, expected int64, actor TransferBatchActor) (ChannelSharingRuleView, error) {
	if !g.paymentWritesEnabled() {
		return ChannelSharingRuleView{}, ErrLiveDisabled
	}
	resolved, resolveErr := g.parentCurrentChannel(ctx, mid, channel)
	if resolveErr != nil {
		return ChannelSharingRuleView{}, resolveErr
	}
	g = resolved
	p, b, e := g.sharingChannel(mid, channel)
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	profile, ok := p.(adapter.ChannelSharingPaymentProfile)
	if !ok || !p.ChannelSharingCapabilities().AutoFinishSupported {
		return ChannelSharingRuleView{}, adapter.ErrUnsupported
	}
	input, e = normalizeSharingRule(input)
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	for _, method := range input.Methods {
		mode, e := profile.ChannelSharingRefundMode(method)
		if e != nil {
			return ChannelSharingRuleView{}, e
		}
		if mode != "merchant_funded" {
			return ChannelSharingRuleView{}, adapter.ErrUnsupported
		}
	}
	account, e := g.Store.sharingDigest("account", []byte(b.AccountScope))
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	return g.Store.setChannelSharingRule(ctx, b, account, input, expected, actor, false)
}

// Disable works against the original binding even when the current provider is
// unavailable or replaced. It cannot enable or rewrite any recipient or method.
func (s *Store) DisableChannelSharingRule(ctx context.Context, mid, channel, expected int64, actor TransferBatchActor) (ChannelSharingRuleView, error) {
	if !actor.valid(mid) || channel <= 0 || expected <= 0 {
		return ChannelSharingRuleView{}, ErrInvalid
	}
	return s.setChannelSharingRule(ctx, channelSharingBinding{MerchantID: mid, ChannelID: channel}, nil, ChannelSharingRuleInput{}, expected, actor, true)
}
func (s *Store) setChannelSharingRule(ctx context.Context, b channelSharingBinding, account []byte, input ChannelSharingRuleInput, expected int64, actor TransferBatchActor, disable bool) (ChannelSharingRuleView, error) {
	if !actor.valid(b.MerchantID) || b.ChannelID <= 0 || expected < 0 {
		return ChannelSharingRuleView{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", b.MerchantID).Scan(&enabled); e != nil || !enabled {
		return ChannelSharingRuleView{}, ErrState
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,48))", fmt.Sprintf("channel-sharing-rule:%d:%d", b.MerchantID, b.ChannelID)); e != nil {
		return ChannelSharingRuleView{}, e
	}
	old, e := scanSharingRule(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingRuleColumns+" FROM channel_sharing_rules WHERE id=(SELECT rule_id FROM channel_sharing_rule_current WHERE merchant_id=$1 AND channel_id=$2)", b.MerchantID, b.ChannelID))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return ChannelSharingRuleView{}, e
	}
	exists := e == nil
	if exists {
		if e = s.verifySharingRule(old); e != nil {
			return ChannelSharingRuleView{}, e
		}
	}
	if disable {
		if !exists {
			return ChannelSharingRuleView{}, ErrNotFound
		}
		if !old.Enabled {
			return old.ChannelSharingRuleView, nil
		}
		b, account, input = old.binding(), old.AccountHash, old.ChannelSharingRuleInput
		input.Enabled = false
	}
	input, e = normalizeSharingRule(input)
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	hash, e := s.sharingRuleFingerprint(b, account, input)
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	if exists && hmac.Equal(hash, old.InputHash) {
		return old.ChannelSharingRuleView, nil
	}
	if expected != old.ID {
		return ChannelSharingRuleView{}, ErrConflict
	}
	// Lock recipients in a deterministic order. Unbind takes the same row lock,
	// preventing an enabled policy from racing a successful remote unbind.
	ids := make([]int64, len(input.Items))
	for i, item := range input.Items {
		ids[i] = item.ReceiverID
	}
	slices.Sort(ids)
	seen := map[string]bool{}
	for _, id := range ids {
		recipient, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE id=$1 AND merchant_id=$2 FOR SHARE", id, b.MerchantID))
		if e != nil {
			return ChannelSharingRuleView{}, e
		}
		rb := recipient.Binding
		if rb.ChannelID != b.ChannelID || rb.Provider != b.Provider || rb.AppID != b.AppID || rb.Product != b.Product || !hmac.Equal(recipient.AccountHash, account) {
			return ChannelSharingRuleView{}, ErrConflict
		}
		if input.Enabled && recipient.State != "bound" {
			return ChannelSharingRuleView{}, ErrState
		}
		raw, e := s.openSharingReceiverRecord(recipient)
		if e != nil {
			return ChannelSharingRuleView{}, e
		}
		key := raw.Type + "\x00" + raw.Account
		if seen[key] {
			return ChannelSharingRuleView{}, ErrConflict
		}
		seen[key] = true
	}
	kind := "channel-sharing-rule"
	if disable {
		kind = "channel-sharing-disable"
	}
	if e = requireFundsAction(ctx, tx, kind, fmt.Sprintf("%d:%d:%d", b.MerchantID, b.ChannelID, expected)); e != nil {
		return ChannelSharingRuleView{}, e
	}
	methods, _ := json.Marshal(input.Methods)
	revision := old.Revision + 1
	var id int64
	var created time.Time
	e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_rules(merchant_id,channel_id,revision,provider,app_id,product,account_hash,input_hash,enabled,minimum_amount,methods,refund_mode,auto_finish,receiver_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'merchant_funded',true,$12) RETURNING id,created_at`, b.MerchantID, b.ChannelID, revision, b.Provider, b.AppID, b.Product, account, hash, input.Enabled, input.MinimumAmount, methods, len(input.Items)).Scan(&id, &created)
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	for i, item := range input.Items {
		if _, e = tx.Exec(ctx, "INSERT INTO channel_sharing_rule_items(rule_id,position,receiver_id,mode,rate_bps,fixed_amount) VALUES($1,$2,$3,$4,$5,$6)", id, i+1, item.ReceiverID, item.Mode, item.RateBPS, item.FixedAmount); e != nil {
			return ChannelSharingRuleView{}, e
		}
	}
	if exists {
		_, e = tx.Exec(ctx, "UPDATE channel_sharing_rule_current SET rule_id=$3 WHERE merchant_id=$1 AND channel_id=$2", b.MerchantID, b.ChannelID, id)
	} else {
		_, e = tx.Exec(ctx, "INSERT INTO channel_sharing_rule_current(merchant_id,channel_id,rule_id) VALUES($1,$2,$3)", b.MerchantID, b.ChannelID, id)
	}
	if e != nil {
		return ChannelSharingRuleView{}, e
	}
	state := "disabled"
	if input.Enabled {
		state = "enabled"
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-rules", id, "configure", state); e != nil {
		return ChannelSharingRuleView{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ChannelSharingRuleView{}, e
	}
	return ChannelSharingRuleView{ID: id, MerchantID: b.MerchantID, ChannelID: b.ChannelID, Revision: revision, Provider: b.Provider, AppID: b.AppID, Product: b.Product, ChannelSharingRuleInput: input, RefundMode: "merchant_funded", AutoFinish: true, CreatedAt: created}, nil
}
