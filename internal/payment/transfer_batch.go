package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"math"
	"strings"
	"time"
)

type TransferBatchActor struct {
	Role, Class, Name string
	ID                int64
}

func (a TransferBatchActor) valid(mid int64) bool {
	return a.ID > 0 && a.Name != "" && len(a.Name) <= 128 && !strings.ContainsAny(a.Name, "\r\n\x00") && (a.Role == "merchant" && a.ID == mid || a.Role == "admin" && (a.Class == "super" || a.Class == "operator"))
}

type TransferBatchInput struct {
	ChannelID int64
	Request   adapter.TransferRequest
}
type TransferBatchCheck func(context.Context, int64) error
type TransferBatchSummary struct {
	Unsent      int `json:"unsent"`
	Processing  int `json:"processing"`
	Unknown     int `json:"unknown"`
	Succeeded   int `json:"succeeded"`
	Failed      int `json:"failed"`
	Cancelled   int `json:"cancelled"`
	Reversed    int `json:"reversed"`
	NeedsReview int `json:"needs_review"`
}
type TransferBatchItem struct {
	Position                         int      `json:"position"`
	SendState                        string   `json:"send_state"`
	Transfer                         Transfer `json:"transfer"`
	ProviderKind, AppID, BankProduct string   `json:"-"`
}
type TransferBatch struct {
	ID           int64                `json:"id,string"`
	MerchantID   int64                `json:"merchant_id,string"`
	ExternalNo   string               `json:"external_no"`
	ItemCount    int                  `json:"item_count"`
	Amount       int64                `json:"amount,string"`
	CostAmount   int64                `json:"cost_amount,string"`
	FeeAmount    int64                `json:"fee_amount,string"`
	CreatedAt    time.Time            `json:"created_at"`
	State        string               `json:"state"`
	AllSucceeded bool                 `json:"all_succeeded"`
	Summary      TransferBatchSummary `json:"summary"`
	Items        []TransferBatchItem  `json:"items"`
}
type batchRoute struct {
	Input                            TransferBatchInput
	ProviderKind, AppID, BankProduct string
	validate                         func(adapter.TransferRequest) error
	AccountScope                     string
}

func (g ProviderFlow) batchRoutes(inputs []TransferBatchInput) ([]batchRoute, error) {
	if len(inputs) == 0 || len(inputs) > 100 {
		return nil, ErrInvalid
	}
	out := make([]batchRoute, 0, len(inputs))
	for _, in := range inputs {
		in.Request.Account = strings.TrimSpace(in.Request.Account)
		in.Request.Name = strings.TrimSpace(in.Request.Name)
		in.Request.TransferNo = ""
		if in.ChannelID <= 0 || !validTransferRequest(in.Request) {
			return nil, ErrInvalid
		}
		p := g.Providers[in.ChannelID]
		if p == nil {
			return nil, adapter.ErrUnsupported
		}
		if _, ok := p.(adapter.TransferProvider); !ok {
			return nil, adapter.ErrUnsupported
		}
		caps, ok := p.(adapter.TransferCapabilities)
		if !ok {
			return nil, adapter.ErrUnsupported
		}
		supported := false
		for _, typ := range caps.TransferTypes() {
			if typ == in.Request.Type {
				supported = true
			}
		}
		if !supported {
			return nil, adapter.ErrUnsupported
		}
		identity, ok := p.(adapter.ApplicationIdentity)
		if !ok || identity.ApplicationID() == "" {
			return nil, adapter.ErrUnsupported
		}
		route := batchRoute{Input: in, ProviderKind: p.ID(), AppID: identity.ApplicationID()}
		var accountErr error
		route.AccountScope, accountErr = adapter.TransferAccountScope(p)
		if accountErr != nil {
			return nil, accountErr
		}
		if route.ProviderKind != "alipay" && route.ProviderKind != "wxpay" {
			return nil, adapter.ErrUnsupported
		}
		if in.Request.Type == "bank" {
			route.BankProduct = providerBankProduct(p)
			if route.BankProduct != adapter.BankProductAlipay && route.BankProduct != adapter.BankProductWechat {
				return nil, adapter.ErrUnsupported
			}
		}
		if v, ok := p.(adapter.TransferValidator); ok {
			route.validate = v.ValidateTransfer
		}
		out = append(out, route)
	}
	return out, nil
}
func (s *Store) batchIntentHash(routes []batchRoute) ([]byte, error) {
	type intent struct {
		ChannelID                    int64
		Request                      adapter.TransferRequest
		Provider, AppID, BankProduct string
	}
	list := make([]intent, 0, len(routes))
	for _, r := range routes {
		list = append(list, intent{r.Input.ChannelID, r.Input.Request, r.ProviderKind, r.AppID, r.BankProduct})
	}
	raw, e := json.Marshal(list)
	if e != nil || len(s.indexKey) != 32 {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.indexKey)
	mac.Write([]byte("goepay/transfer-batch/v1\x00"))
	mac.Write(raw)
	return mac.Sum(nil), nil
}
func batchEventTx(ctx context.Context, tx pgx.Tx, batchID, transferID int64, action, state string, actor TransferBatchActor) error {
	var id any
	if transferID > 0 {
		id = transferID
	}
	if _, e := tx.Exec(ctx, "INSERT INTO transfer_batch_events(batch_id,transfer_id,action,actor_role,actor_id,actor_name,result_state) VALUES($1,$2,$3,$4,$5,$6,$7)", batchID, id, action, actor.Role, actor.ID, actor.Name, state); e != nil {
		return e
	}
	raw, _ := json.Marshal(map[string]any{"batch_id": fmt.Sprint(batchID), "transfer_id": fmt.Sprint(transferID), "state": state})
	_, e := tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'transfer-batches',$4,$5,$6)", actor.Role, actor.ID, actor.Name, batchID, "batch-"+action, raw)
	return e
}
func (g ProviderFlow) PrepareTransferBatch(ctx context.Context, mid int64, no string, inputs []TransferBatchInput, actor TransferBatchActor) (TransferBatch, error) {
	return g.PrepareTransferBatchChecked(ctx, mid, no, inputs, actor, nil)
}
func (g ProviderFlow) PrepareTransferBatchChecked(ctx context.Context, mid int64, no string, inputs []TransferBatchInput, actor TransferBatchActor, check TransferBatchCheck) (TransferBatch, error) {
	if g.Store == nil || !actor.valid(mid) || no == "" || len(no) > 128 || strings.TrimSpace(no) != no || strings.ContainsAny(no, "\r\n\x00") {
		return TransferBatch{}, ErrInvalid
	}
	routes, e := g.batchRoutes(inputs)
	if e != nil {
		return TransferBatch{}, e
	}
	for _, r := range routes {
		if check != nil {
			if e = check(ctx, r.Input.ChannelID); e != nil {
				return TransferBatch{}, e
			}
		}
	}
	digest, e := g.Store.batchIntentHash(routes)
	if e != nil {
		return TransferBatch{}, e
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return TransferBatch{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&enabled); e != nil {
		return TransferBatch{}, e
	}
	if !enabled {
		return TransferBatch{}, ErrState
	}
	var id int64
	var old []byte
	e = tx.QueryRow(ctx, "SELECT id,intent_hash FROM transfer_batches WHERE merchant_id=$1 AND external_no=$2", mid, no).Scan(&id, &old)
	if e == nil {
		if !hmac.Equal(old, digest) {
			return TransferBatch{}, ErrConflict
		}
		// Preserve the original request digest format. The immutable account
		// proofs add the payer dimension without rewriting historical intents.
		rows, err := tx.Query(ctx, "SELECT transfer_id,position FROM transfer_batch_items WHERE batch_id=$1 ORDER BY position", id)
		if err != nil {
			return TransferBatch{}, err
		}
		type boundItem struct {
			id       int64
			position int
		}
		items := make([]boundItem, 0, len(routes))
		for rows.Next() {
			var item boundItem
			if err = rows.Scan(&item.id, &item.position); err != nil {
				rows.Close()
				return TransferBatch{}, err
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return TransferBatch{}, err
		}
		if len(items) != len(routes) {
			return TransferBatch{}, ErrConflict
		}
		for pos, item := range items {
			if item.position != pos+1 {
				return TransferBatch{}, ErrConflict
			}
			if err = g.Store.checkTransferAccountScope(ctx, tx, item.id, routes[pos].AccountScope, true); err != nil {
				return TransferBatch{}, err
			}
		}
		if e = tx.Commit(ctx); e != nil {
			return TransferBatch{}, e
		}
		return g.Store.TransferBatch(ctx, mid, id)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return TransferBatch{}, e
	}
	if e = requireFundsAction(ctx, tx, "batch-prepare", fmt.Sprintf("%d:%s", mid, no)); e != nil {
		return TransferBatch{}, e
	}
	// The whole immutable batch is authorized once, in this same transaction.
	// Its component reservations must not consume that grant a second time.
	ctx = WithFundsActionGuard(ctx, nil)
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('transfer_batches','id'))").Scan(&id); e != nil {
		return TransferBatch{}, e
	}
	transfers := make([]Transfer, 0, len(routes))
	var amount, cost int64
	for pos, r := range routes {
		if check != nil {
			if e = check(ctx, r.Input.ChannelID); e != nil {
				return TransferBatch{}, e
			}
		}
		t, owner, err := g.Store.beginTransferTx(ctx, tx, mid, r.Input.ChannelID, fmt.Sprintf("TB%d-%d", id, pos+1), r.Input.Request, r.validate, r.BankProduct, r.AccountScope)
		if err != nil {
			return TransferBatch{}, err
		}
		if !owner {
			return TransferBatch{}, ErrConflict
		}
		if t.Amount > math.MaxInt64-amount || t.CostAmount > math.MaxInt64-cost {
			return TransferBatch{}, ErrInvalid
		}
		amount += t.Amount
		cost += t.CostAmount
		transfers = append(transfers, t)
		if _, e = tx.Exec(ctx, "DELETE FROM jobs WHERE event_key=$1 AND kind='transfer.reconcile'", fmt.Sprintf("transfer:%d", t.ID)); e != nil {
			return TransferBatch{}, e
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO transfer_batches(id,merchant_id,external_no,intent_hash,item_count,amount,cost_amount) VALUES($1,$2,$3,$4,$5,$6,$7)", id, mid, no, digest, len(routes), amount, cost); e != nil {
		return TransferBatch{}, e
	}
	for pos, t := range transfers {
		r := routes[pos]
		if _, e = tx.Exec(ctx, "INSERT INTO transfer_batch_items(batch_id,position,transfer_id,provider_kind,app_id,bank_product) VALUES($1,$2,$3,$4,$5,$6)", id, pos+1, t.ID, r.ProviderKind, r.AppID, r.BankProduct); e != nil {
			return TransferBatch{}, e
		}
	}
	if e = batchEventTx(ctx, tx, id, 0, "prepare", "unsent", actor); e != nil {
		return TransferBatch{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return TransferBatch{}, e
	}
	return g.Store.TransferBatch(ctx, mid, id)
}

func (s *Store) TransferBatch(ctx context.Context, mid, id int64) (TransferBatch, error) {
	var b TransferBatch
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return b, e
	}
	defer tx.Rollback(ctx)
	e = tx.QueryRow(ctx, "SELECT id,merchant_id,external_no,item_count,amount,cost_amount,created_at FROM transfer_batches WHERE id=$1 AND merchant_id=$2", id, mid).Scan(&b.ID, &b.MerchantID, &b.ExternalNo, &b.ItemCount, &b.Amount, &b.CostAmount, &b.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	if e != nil {
		return b, e
	}
	b.FeeAmount = b.CostAmount - b.Amount
	rows, e := tx.Query(ctx, "SELECT position,transfer_id,state,provider_kind,app_id,bank_product FROM transfer_batch_items WHERE batch_id=$1 ORDER BY position", id)
	if e != nil {
		return b, e
	}
	var ids []int64
	for rows.Next() {
		var item TransferBatchItem
		var transferID int64
		if e = rows.Scan(&item.Position, &transferID, &item.SendState, &item.ProviderKind, &item.AppID, &item.BankProduct); e != nil {
			rows.Close()
			return b, e
		}
		b.Items = append(b.Items, item)
		ids = append(ids, transferID)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return b, e
	}
	for i, transferID := range ids {
		t, e := s.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1 AND t.merchant_id=$2", transferID, mid))
		if e != nil {
			return b, e
		}
		b.Items[i].Transfer = t
		switch b.Items[i].SendState {
		case "unsent":
			b.Summary.Unsent++
			b.Items[i].Transfer.Status = "unsent"
		case "cancelled":
			b.Summary.Cancelled++
			b.Items[i].Transfer.Status = "cancelled"
		default:
			switch t.Status {
			case "succeeded":
				b.Summary.Succeeded++
			case "failed":
				b.Summary.Failed++
			case "unknown":
				b.Summary.Unknown++
			case "reversed":
				b.Summary.Reversed++
			case "reversal_pending":
				b.Summary.NeedsReview++
			default:
				b.Summary.Processing++
			}
		}
	}
	b.State = "pending"
	if b.Summary.Unsent == b.ItemCount {
		b.State = "prepared"
	}
	if b.Summary.Succeeded+b.Summary.Failed+b.Summary.Cancelled+b.Summary.Reversed == b.ItemCount {
		b.State = "finished"
	}
	b.AllSucceeded = b.Summary.Succeeded == b.ItemCount
	return b, tx.Commit(ctx)
}
