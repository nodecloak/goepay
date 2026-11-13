package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

func cryptoError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, cryptopay.ErrNotFound):
		response(w, 404, -1, "USDT 记录不存在或无权查看", nil)
	case errors.Is(e, cryptopay.ErrInvalid):
		response(w, 422, -1, "USDT 请求无效，请核对网络、地址及金额", nil)
	case errors.Is(e, cryptopay.ErrConflict):
		response(w, 409, -1, "USDT 操作与原请求或当前状态冲突，请核对记录、地址审批及冷静期", nil)
	case errors.Is(e, cryptopay.ErrInsufficient):
		response(w, 422, -1, "USDT 可用资金不足", nil)
	case errors.Is(e, errCredentialActor199), errors.Is(e, identity.ErrFundsAuthentication):
		response(w, 403, -1, "original active account session required", nil)
	case errors.Is(e, errCurrentPassword), errors.Is(e, errCredentialUnset199):
		response(w, 422, -1, "当前登录密码不正确或尚未设置", nil)
	default:
		response(w, 503, -1, "USDT 结果暂无法确认，请先查询原请求记录，勿重复提交", nil)
	}
}

func cryptoMerchant(role string, session identity.Session, supplied string) (int64, error) {
	if role != "admin" && (role != "merchant" || session.User.ID <= 0) {
		return 0, errCredentialActor199
	}
	if supplied == "" {
		if role == "merchant" {
			return session.User.ID, nil
		}
		return 0, nil
	}
	mid, e := strconv.ParseInt(supplied, 10, 64)
	if e != nil || mid <= 0 {
		return 0, cryptopay.ErrInvalid
	}
	if role == "merchant" && mid != session.User.ID {
		return 0, errCredentialActor199
	}
	return mid, nil
}

// Every HTTP mutation carries the current durable account/session proof into
// the crypto transaction. The password is checked only in memory and is never
// placed in a business record, audit, checkout token, or response.
func (s *Server) cryptoContext(w http.ResponseWriter, r *http.Request, role string, session identity.Session, password string, confirm bool) (context.Context, bool) {
	a, e := originalCredentialActor199(r, role, session)
	if e != nil {
		cryptoError(w, e)
		return nil, false
	}
	if confirm {
		if len(password) == 0 || len(password) > 1024 {
			cryptoError(w, errCurrentPassword)
			return nil, false
		}
		if !s.credentialBudget199(w, r, a) {
			return nil, false
		}
	}
	checked := false
	ctx := cryptopay.WithAuthority(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		account, err := s.credentialAccountTx199(ctx, tx, a)
		if err != nil {
			return err
		}
		if role == "admin" {
			if err = requireConsoleAdminAuthorityTx(ctx, tx); err != nil {
				return err
			}
		}
		if confirm && !checked {
			checked = true
			return checkCredentialPassword199(account.hash, password)
		}
		return nil
	})
	return ctx, true
}

func (s *Server) cryptoConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "crypto" {
		return false
	}
	if role == "admin" && session.User.AdminClass != "super" {
		response(w, 403, -1, "super administrator required", nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "config" && r.Method == "GET" {
		networks := make([]map[string]any, 0)
		enabled := false
		if s.Crypto != nil {
			for _, name := range []string{"tron", "ethereum"} {
				if n, ok := s.Crypto.Networks[name]; ok {
					networks = append(networks, map[string]any{"network": name, "enabled": n.Enabled, "fee_bps": n.FeeBPS})
				}
			}
		}
		if s.Crypto != nil {
			for _, n := range s.Crypto.Networks {
				enabled = enabled || n.Enabled
			}
		}
		response(w, 200, 0, "", map[string]any{"enabled": enabled, "networks": networks, "simulation": s.Sandbox})
		return true
	}
	if s.Crypto == nil {
		cryptoError(w, cryptopay.ErrUnavailable)
		return true
	}
	if r.Method == "GET" {
		mid, e := cryptoMerchant(role, session, r.URL.Query().Get("merchant_id"))
		if e != nil {
			cryptoError(w, e)
			return true
		}
		var data any
		switch {
		case len(parts) == 3 && parts[2] == "balances":
			data, e = s.Crypto.Balances(r.Context(), mid)
		case len(parts) == 3 && parts[2] == "invoices":
			data, e = s.Crypto.ListInvoices(r.Context(), mid)
		case len(parts) == 4 && parts[2] == "invoices":
			var invoice cryptopay.Invoice
			invoice, e = s.Crypto.GetInvoice(r.Context(), mid, parts[3])
			if e == nil {
				data = cryptoInvoiceResponse(invoice)
			}
		case len(parts) == 3 && parts[2] == "withdrawals":
			data, e = s.Crypto.ListWithdrawals(r.Context(), mid)
		case len(parts) == 4 && parts[2] == "withdrawals" && parts[3] == "by-request":
			data, e = s.Crypto.FindWithdrawal(r.Context(), mid, r.URL.Query().Get("request_id"))
		case len(parts) == 3 && parts[2] == "recipients":
			data, e = s.Crypto.ListRecipients(r.Context(), mid)
		case len(parts) == 3 && parts[2] == "reconciliation" && role == "admin":
			data, e = s.Crypto.Reconcile(r.Context())
		default:
			response(w, 404, -1, "USDT route not found", nil)
			return true
		}
		if e != nil {
			cryptoError(w, e)
		} else {
			response(w, 200, 0, "", data)
		}
		return true
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	var e error
	var data any
	var financialMerchant int64
	var financialRequest string
	switch {
	case len(parts) == 4 && parts[2] == "sweeps" && parts[3] == "retry" && role == "admin":
		var in struct {
			Network  string `json:"network"`
			Address  string `json:"address"`
			Password string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		e = s.Crypto.ResumeSweep(ctx, in.Network, in.Address, session.User.ID)
	case len(parts) == 5 && (parts[2] == "withdrawals" && parts[4] == "cancel" || parts[2] == "recipients" && parts[4] == "revoke"):
		var in struct {
			MerchantID string `json:"merchant_id"`
			Password   string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		mid, err := cryptoMerchant(role, session, in.MerchantID)
		if err != nil || mid <= 0 {
			if err == nil {
				err = cryptopay.ErrInvalid
			}
			cryptoError(w, err)
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		if parts[2] == "withdrawals" {
			data, e = s.Crypto.CancelWithdrawal(ctx, mid, parts[3])
		} else {
			e = s.Crypto.RevokeRecipient(ctx, parts[3], mid)
		}
	case len(parts) == 3 && parts[2] == "recipients":
		var in struct {
			MerchantID string `json:"merchant_id"`
			Network    string `json:"network"`
			Address    string `json:"address"`
			Password   string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		mid, err := cryptoMerchant(role, session, in.MerchantID)
		if err != nil || mid <= 0 {
			if err == nil {
				err = cryptopay.ErrInvalid
			}
			cryptoError(w, err)
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		data, e = s.Crypto.RequestRecipient(ctx, mid, in.Network, in.Address)
	case len(parts) == 5 && parts[2] == "recipients" && parts[4] == "approve" && role == "admin":
		var in struct {
			Password string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		data, e = s.Crypto.ApproveRecipient(ctx, parts[3], session.User.ID)
	case len(parts) == 3 && parts[2] == "invoices":
		var in struct {
			MerchantID string `json:"merchant_id"`
			OutTradeNo string `json:"out_trade_no"`
			Network    string `json:"network"`
			Amount     string `json:"amount"`
			NotifyURL  string `json:"notify_url"`
			ReturnURL  string `json:"return_url"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		var mid int64
		mid, e = cryptoMerchant(role, session, in.MerchantID)
		if e != nil || mid <= 0 {
			if e == nil {
				e = cryptopay.ErrInvalid
			}
			cryptoError(w, e)
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, "", false)
		if !ok {
			return true
		}
		var invoice cryptopay.Invoice
		invoice, e = s.Crypto.CreateInvoice(ctx, cryptopay.CreateInvoiceInput{MerchantID: mid, OutTradeNo: in.OutTradeNo, Network: in.Network, Amount: in.Amount, NotifyURL: in.NotifyURL, ReturnURL: in.ReturnURL, ExpiresIn: 15 * time.Minute, Name: "USDT", Version: "native"})
		if e == nil {
			data = cryptoInvoiceResponse(invoice)
		}
	case len(parts) == 3 && parts[2] == "withdrawals":
		var in struct {
			MerchantID string `json:"merchant_id"`
			RequestID  string `json:"request_id"`
			Network    string `json:"network"`
			Address    string `json:"address"`
			Amount     string `json:"amount"`
			Password   string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		var mid int64
		mid, e = cryptoMerchant(role, session, in.MerchantID)
		if e != nil || mid <= 0 {
			if e == nil {
				e = cryptopay.ErrInvalid
			}
			cryptoError(w, e)
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		financialMerchant, financialRequest = mid, in.RequestID
		data, e = s.Crypto.RequestWithdrawal(ctx, cryptopay.WithdrawalInput{MerchantID: mid, RequestID: in.RequestID, Network: in.Network, Address: in.Address, Amount: in.Amount})
	case len(parts) == 3 && (parts[2] == "refunds" || parts[2] == "held-refunds"):
		var in struct {
			MerchantID string `json:"merchant_id"`
			RequestID  string `json:"request_id"`
			InvoiceID  string `json:"invoice_id"`
			Address    string `json:"address"`
			Amount     string `json:"amount"`
			Password   string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		var mid int64
		mid, e = cryptoMerchant(role, session, in.MerchantID)
		if e != nil || mid <= 0 {
			if e == nil {
				e = cryptopay.ErrInvalid
			}
			cryptoError(w, e)
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		input := cryptopay.RefundInput{MerchantID: mid, RequestID: in.RequestID, InvoiceID: in.InvoiceID, Address: in.Address, Amount: in.Amount}
		financialMerchant, financialRequest = mid, in.RequestID
		if parts[2] == "held-refunds" {
			data, e = s.Crypto.RefundHeldFunds(ctx, input)
		} else {
			data, e = s.Crypto.Refund(ctx, input)
		}
	case len(parts) == 5 && parts[2] == "withdrawals" && parts[4] == "approve" && role == "admin":
		var in struct {
			Password string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		data, e = s.Crypto.ApproveWithdrawal(ctx, parts[3], session.User.ID)
	case len(parts) == 3 && parts[2] == "addresses" && role == "admin":
		var in struct {
			Network  string `json:"network"`
			Address  string `json:"address"`
			Role     string `json:"role"`
			Password string `json:"current_password"`
		}
		if !decodeFundsJSON(w, r, &in) {
			return true
		}
		ctx, ok := s.cryptoContext(w, r, role, session, in.Password, true)
		if !ok {
			return true
		}
		defer releaseLoginVerification()
		e = s.Crypto.AddAddress(ctx, in.Network, in.Address, in.Role)
	default:
		response(w, 404, -1, "USDT route not found", nil)
		return true
	}
	if e != nil {
		if errors.Is(e, cryptopay.ErrConflict) && financialMerchant > 0 && financialRequest != "" {
			if _, lookupError := s.Crypto.FindWithdrawal(r.Context(), financialMerchant, financialRequest); errors.Is(lookupError, cryptopay.ErrNotFound) {
				response(w, 409, -1, "USDT 当前状态不允许该操作；本次请求未记录", map[string]any{"request_not_recorded": true, "kind": "crypto", "merchant_id": strconv.FormatInt(financialMerchant, 10), "external_no": financialRequest, "reason": "conflict"})
				return true
			}
		}
		cryptoError(w, e)
	} else {
		response(w, 200, 0, "", data)
	}
	return true
}

func cryptoInvoiceResponse(v cryptopay.Invoice) any {
	return struct {
		cryptopay.Invoice
		CheckoutURL string `json:"checkout_url"`
	}{v, "/crypto/checkout/" + url.PathEscape(v.ID) + "?" + url.Values{"token": {v.CheckoutToken}}.Encode()}
}
