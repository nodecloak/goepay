package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ChannelSharingRuleOverviewQuery struct {
	MerchantID, ChannelID, RuleID, ReceiverID int64
	Enabled                                   *bool
	Offset, Limit                             int
}

type ChannelSharingRuleReceiverSummary struct {
	ID            int64  `json:"id,string"`
	AccountMasked string `json:"account_masked"`
	NameMasked    string `json:"name_masked"`
	State         string `json:"state"`
}

type ChannelSharingRuleOverviewView struct {
	Rule      ChannelSharingRuleView              `json:"rule"`
	Receivers []ChannelSharingRuleReceiverSummary `json:"receivers"`
}

type ChannelSharingRuleOverviewPage struct {
	Items []ChannelSharingRuleOverviewView `json:"items"`
	Total int64                            `json:"total"`
	Limit int                              `json:"limit"`
}

// Overview is a separate administrator read capability. Missing merchant scope
// remains invalid for the original per-merchant methods and all writes.
func adminSharingOverviewActor(actor TransferBatchActor) bool {
	return actor.Role == "admin" && actor.valid(1)
}

func (s *Store) ListAdminChannelSharingOrders(ctx context.Context, q ChannelSharingOrderQuery, actor TransferBatchActor) (ChannelSharingOrderListPage, error) {
	return s.listChannelSharingOrders(ctx, q, actor, true)
}

func (s *Store) AdminChannelSharingOrderStatistics(ctx context.Context, q ChannelSharingOrderQuery, actor TransferBatchActor) (ChannelSharingOrderStats, error) {
	return s.channelSharingOrderStatistics(ctx, q, actor, true)
}

func (q ChannelSharingOrderQuery) AdminChannelSharingOrderCSVQuery(actor TransferBatchActor) (string, []any, bool) {
	return q.channelSharingOrderCSVQuery(actor, true)
}

// Current pointers, immutable rule items and masked receivers are read in one
// repeatable-read snapshot. Four bounded queries avoid per-row provider calls
// and keep saved rules visible when a channel is disabled or unavailable.
func (s *Store) ListAdminChannelSharingRules(ctx context.Context, q ChannelSharingRuleOverviewQuery, actor TransferBatchActor) (ChannelSharingRuleOverviewPage, error) {
	out := ChannelSharingRuleOverviewPage{Items: []ChannelSharingRuleOverviewView{}, Limit: q.Limit}
	if !adminSharingOverviewActor(actor) || q.MerchantID < 0 || q.ChannelID < 0 || q.RuleID < 0 || q.ReceiverID < 0 || q.Offset < 0 || q.Offset > 1_000_000 || q.Limit < 1 || q.Limit > 100 {
		return out, ErrInvalid
	}
	args := []any{}
	conditions := []string{"TRUE"}
	add := func(cond string, v any) {
		args = append(args, v)
		conditions = append(conditions, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if q.MerchantID > 0 {
		add("r.merchant_id=?", q.MerchantID)
	}
	if q.ChannelID > 0 {
		add("r.channel_id=?", q.ChannelID)
	}
	if q.RuleID > 0 {
		add("r.id=?", q.RuleID)
	}
	if q.ReceiverID > 0 {
		add("EXISTS (SELECT 1 FROM channel_sharing_rule_items i WHERE i.rule_id=r.id AND i.receiver_id=?)", q.ReceiverID)
	}
	if q.Enabled != nil {
		add("r.enabled=?", *q.Enabled)
	}
	from := ` FROM channel_sharing_rule_current c JOIN channel_sharing_rules r ON r.id=c.rule_id AND r.merchant_id=c.merchant_id AND r.channel_id=c.channel_id WHERE ` + strings.Join(conditions, " AND ")
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT count(*)"+from, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	pageArgs := append(append([]any{}, args...), q.Limit, q.Offset)
	rows, err := tx.Query(ctx, "SELECT r."+strings.ReplaceAll(sharingRuleColumns, ",", ",r.")+from+" ORDER BY r.id DESC LIMIT $"+strconv.Itoa(len(args)+1)+" OFFSET $"+strconv.Itoa(len(args)+2), pageArgs...)
	if err != nil {
		return out, err
	}
	records := []sharingRuleRecord{}
	ids := []int64{}
	byID := map[int64]int{}
	for rows.Next() {
		var r sharingRuleRecord
		var methods []byte
		if err = rows.Scan(&r.ID, &r.MerchantID, &r.ChannelID, &r.Revision, &r.Provider, &r.AppID, &r.Product, &r.AccountHash, &r.InputHash, &r.Enabled, &r.MinimumAmount, &methods, &r.RefundMode, &r.AutoFinish, &r.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		if json.Unmarshal(methods, &r.Methods) != nil {
			rows.Close()
			return out, ErrInvalid
		}
		byID[r.ID] = len(records)
		ids = append(ids, r.ID)
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(records) == 0 {
		return out, tx.Commit(ctx)
	}
	rows, err = tx.Query(ctx, "SELECT rule_id,receiver_id,mode,rate_bps,fixed_amount FROM channel_sharing_rule_items WHERE rule_id=ANY($1::bigint[]) ORDER BY rule_id,position", ids)
	if err != nil {
		return out, err
	}
	receiverIDs := []int64{}
	for rows.Next() {
		var ruleID int64
		var item ChannelSharingRuleItem
		if err = rows.Scan(&ruleID, &item.ReceiverID, &item.Mode, &item.RateBPS, &item.FixedAmount); err != nil {
			rows.Close()
			return out, err
		}
		index := byID[ruleID]
		records[index].Items = append(records[index].Items, item)
		receiverIDs = append(receiverIDs, item.ReceiverID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE id=ANY($1::bigint[])", receiverIDs)
	if err != nil {
		return out, err
	}
	receivers := map[int64]sharingReceiverRecord{}
	summaries := map[int64]ChannelSharingRuleReceiverSummary{}
	for rows.Next() {
		r, e := scanSharingReceiver(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		input, e := s.openSharingReceiverRecord(r)
		if e != nil {
			rows.Close()
			return out, e
		}
		view := sharingReceiverView(r, input)
		receivers[r.ID] = r
		summaries[r.ID] = ChannelSharingRuleReceiverSummary{ID: r.ID, AccountMasked: view.AccountMasked, NameMasked: view.NameMasked, State: view.State}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, r := range records {
		if _, err = normalizeSharingRule(r.ChannelSharingRuleInput); err != nil {
			return out, err
		}
		if err = s.verifySharingRule(r); err != nil {
			return out, err
		}
		view := ChannelSharingRuleOverviewView{Rule: r.ChannelSharingRuleView, Receivers: []ChannelSharingRuleReceiverSummary{}}
		for _, item := range r.Items {
			receiver, ok := receivers[item.ReceiverID]
			b := receiver.Binding
			if !ok || b.MerchantID != r.MerchantID || b.ChannelID != r.ChannelID || b.Provider != r.Provider || b.AppID != r.AppID || b.Product != r.Product || !hmac.Equal(receiver.AccountHash, r.AccountHash) {
				return out, ErrConflict
			}
			view.Receivers = append(view.Receivers, summaries[item.ReceiverID])
		}
		out.Items = append(out.Items, view)
	}
	return out, tx.Commit(ctx)
}
