package backoffice

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This read resolves a browser's original intent without scanning a merchant's
// unrelated transactions or exposing a counterparty's private request number.
// The console's existing session/menu middleware still governs this family.
func (s *Service) serveWorkflowIntent212(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) != 2 || parts[1] != "lookup" || (parts[0] != "settlements" && parts[0] != "internal-transfers") {
		return false
	}
	if r.Method != "GET" {
		write(w, 405, nil, "method not allowed")
		return true
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		write(w, http.StatusBadRequest, nil, "invalid original request lookup")
		return true
	}
	for k, v := range q {
		if len(v) != 1 || (k != "external_no" && k != "merchant_id") {
			write(w, http.StatusBadRequest, nil, "invalid original request lookup")
			return true
		}
	}
	external := q.Get("external_no")
	if external == "" || len(external) > 128 || !utf8.ValidString(external) || strings.TrimSpace(external) != external || strings.ContainsAny(external, "\x00\r\n") {
		write(w, http.StatusBadRequest, nil, "invalid original request lookup")
		return true
	}
	mid := p.SubjectID
	if raw := q.Get("merchant_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
			write(w, http.StatusBadRequest, nil, "invalid original request lookup")
			return true
		}
		if p.Role != "admin" && id != mid {
			writeError(w, ErrForbidden)
			return true
		}
		mid = id
	} else if p.Role == "admin" {
		write(w, http.StatusBadRequest, nil, "invalid original request lookup")
		return true
	}
	if s.pool == nil || s.store == nil {
		write(w, 503, nil, "lookup unavailable")
		return true
	}
	var id int64
	e = s.pool.QueryRow(r.Context(), "SELECT id FROM ledger_requests WHERE kind=$1 AND merchant_id=$2 AND external_no=$3", parts[0], mid, external).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		writeError(w, ErrNotFound)
		return true
	}
	if e != nil {
		writeError(w, e)
		return true
	}
	// The persisted request reader includes the original recipient version and
	// fee snapshot, so the browser can verify rather than infer the intent.
	var v payment.LedgerRequest
	if parts[0] == "settlements" {
		v, e = s.store.SettlementRequest(r.Context(), mid, id)
	} else {
		e = s.pool.QueryRow(r.Context(), "SELECT id,kind,merchant_id,recipient_id,external_no,amount,status,created_at FROM ledger_requests WHERE id=$1 AND merchant_id=$2 AND kind='internal-transfers'", id, mid).Scan(&v.ID, &v.Kind, &v.MerchantID, &v.RecipientID, &v.ExternalNo, &v.Amount, &v.Status, &v.CreatedAt)
	}
	if e != nil {
		writeError(w, e)
		return true
	}
	if v.Kind != parts[0] || v.MerchantID != mid || v.ExternalNo != external {
		writeError(w, payment.ErrNotFound)
		return true
	}
	write(w, 200, requestJSON(v), "original request recorded; approval is not external payment")
	return true
}

// RequestFunds has completed and rolled back before this check. Never describe
// an idempotency collision as an absent request. If the proof read fails, keep
// the conservative legacy error and let the browser retain its original intent.
func (s *Service) writeWorkflowRefusal212(w http.ResponseWriter, r *http.Request, kind string, mid int64, external string, cause error) bool {
	if (kind != "settlements" && kind != "internal-transfers") || (!errors.Is(cause, payment.ErrConflict) && !errors.Is(cause, payment.ErrInsufficient)) {
		return false
	}
	var exists bool
	if e := s.pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM ledger_requests WHERE kind=$1 AND merchant_id=$2 AND external_no=$3)", kind, mid, external).Scan(&exists); e != nil || exists {
		return false
	}
	reason := "conflict"
	if errors.Is(cause, payment.ErrInsufficient) {
		reason = "insufficient"
	}
	write(w, 409, map[string]any{"request_not_recorded": true, "kind": kind, "merchant_id": strconv.FormatInt(mid, 10), "external_no": external, "reason": reason}, "原申请未登记：费用、收款版本或可用余额已变化，请重新核对后提交")
	return true
}
