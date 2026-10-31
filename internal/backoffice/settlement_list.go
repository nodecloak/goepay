package backoffice

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"goepay/internal/privatesearch"
	"net/http"
)

func (s *Service) serveOriginalSettlementList(w http.ResponseWriter, r *http.Request, p Principal, statistics bool) {
	f, e := parseSettlementListFilter(r, p)
	if e != nil {
		writeError(w, e)
		return
	}
	tx, e := s.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		writeError(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	where := settlementListWhereSQL
	filterArgs := f.args()
	if f.Search != "" {
		ids, err := s.store.PrivateSearchIDs(r.Context(), tx, "settlement_config", f.MerchantID, []privatesearch.Field{privatesearch.Account, privatesearch.Name}, f.Search)
		if err != nil {
			if errors.Is(err, payment.ErrPrivateSearchBroad) {
				write(w, 422, nil, "搜索范围过大，请输入更完整的账号或姓名")
			} else {
				writeError(w, err)
			}
			return
		}
		filterArgs = append(filterArgs, ids)
		where += " AND id IN(SELECT request_id FROM settlement_recipients WHERE config_id=ANY($6::bigint[]))"
	}
	if statistics {
		var total, money, waiting, complete, processing, failed, cancelled, returning, returned string
		e = tx.QueryRow(r.Context(), settlementListRowsSQL+`SELECT count(*)::text,(COALESCE(sum(amount::numeric),0)/100)::numeric(38,2)::text,
   (count(*) FILTER(WHERE original_state=0))::text,(count(*) FILTER(WHERE original_state=1))::text,
   (count(*) FILTER(WHERE original_state=2))::text,(count(*) FILTER(WHERE original_state=3))::text,
   (count(*) FILTER(WHERE original_state=4))::text,(count(*) FILTER(WHERE original_state=5))::text,
   (count(*) FILTER(WHERE original_state=6))::text FROM settlement_rows `+where, filterArgs...).Scan(&total, &money, &waiting, &complete, &processing, &failed, &cancelled, &returning, &returned)
		if e != nil {
			writeError(w, e)
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, map[string]string{"total_count": total, "total_money": money, "waiting_count": waiting, "completed_count": complete, "processing_count": processing, "failed_count": failed, "cancelled_count": cancelled, "returning_count": returning, "returned_count": returned}, "")
		return
	}
	var total int64
	if e = tx.QueryRow(r.Context(), settlementListRowsSQL+"SELECT count(*) FROM settlement_rows "+where, filterArgs...).Scan(&total); e != nil {
		writeError(w, e)
		return
	}
	args := append(filterArgs, f.Limit, (f.Page-1)*f.Limit)
	rows, e := tx.Query(r.Context(), settlementListRowsSQL+"SELECT id,merchant_id,recipient_id,external_no,amount,status,created_at,"+payment.SettlementTermsJSON+",(settlement_plan_item_id IS NOT NULL OR EXISTS(SELECT 1 FROM ledger_request_events e WHERE e.request_id=ledger_requests.id AND e.actor='system:settlement-scheduler')) AS auto FROM settlement_rows ledger_requests "+where+fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(filterArgs)+1, len(filterArgs)+2), args...)
	if e != nil {
		writeError(w, e)
		return
	}
	requests := []payment.LedgerRequest{}
	autoByID := map[int64]bool{}
	for rows.Next() {
		v := payment.LedgerRequest{Kind: "settlements"}
		var terms []byte
		var auto bool
		if e = rows.Scan(&v.ID, &v.MerchantID, &v.RecipientID, &v.ExternalNo, &v.Amount, &v.Status, &v.CreatedAt, &terms, &auto); e != nil {
			rows.Close()
			writeError(w, e)
			return
		}
		autoByID[v.ID] = auto
		if len(terms) > 0 {
			if e = json.Unmarshal(terms, &v.Settlement); e != nil {
				rows.Close()
				writeError(w, e)
				return
			}
		}
		requests = append(requests, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		writeError(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		writeError(w, e)
		return
	}
	items := []map[string]any{}
	for _, v := range requests {
		v, e = s.store.SettlementRequest(r.Context(), v.MerchantID, v.ID)
		if e != nil {
			writeError(w, e)
			return
		}
		var payout *payment.SettlementPayout
		original, pe := s.store.SettlementPayout(r.Context(), v.ID)
		if pe == nil {
			payout = &original
		} else if !errors.Is(pe, payment.ErrNotFound) {
			writeError(w, pe)
			return
		}
		item := requestJSON(v)
		item["payout"] = payout
		item["auto"] = autoByID[v.ID]
		items = append(items, item)
	}
	if e = s.attachSettlementReceiptCapabilities(r.Context(), items); e != nil {
		writeError(w, e)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total, "page": f.Page, "limit": f.Limit}, "")
}
