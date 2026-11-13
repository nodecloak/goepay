package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"goepay/internal/cryptopay"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

// A collection channel has a shared console identity, but never impersonates a
// CNY provider account. Its capability determines the executable money path.
type cryptoChannelView struct {
	cryptopay.Channel
	PayType        string `json:"pay_type"`
	Plugin         string `json:"plugin"`
	Configured     bool   `json:"configured"`
	Asset          string `json:"asset"`
	RuntimeEnabled bool   `json:"runtime_enabled"`
}

func (s *Server) cryptoChannelView(c cryptopay.Channel) cryptoChannelView {
	enabled := s.Crypto != nil && s.Crypto.Networks[c.Network].Enabled
	return cryptoChannelView{Channel: c, PayType: "usdt", Plugin: "usdt_" + c.Network, Configured: true, Asset: "USDT", RuntimeEnabled: enabled}
}

func (s *Server) cryptoChannelContext(ctx context.Context, session identity.Session, token string) context.Context {
	a := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	return cryptopay.WithAuthority(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.Store.CheckParentChannelActorTx(ctx, tx, a)
	})
}

func (s *Server) cryptoChannelList(ctx context.Context) ([]cryptopay.Channel, error) {
	if s.Pool == nil {
		return nil, cryptopay.ErrUnavailable
	}
	var installed bool
	if err := s.Pool.QueryRow(ctx, `SELECT to_regclass('crypto_channels') IS NOT NULL`).Scan(&installed); err != nil {
		return nil, err
	}
	// Old fixture schemas have no crypto catalog. Any actual read error remains
	// an error and cannot be converted into an empty successful response.
	if !installed {
		return []cryptopay.Channel{}, nil
	}
	return (&cryptopay.Service{Pool: s.Pool}).ListChannels(ctx)
}

func cryptoChannelError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrAdminSession):
		response(w, 403, -1, "需要当前超级管理员本人会话", nil)
	case errors.Is(err, cryptopay.ErrInvalid):
		response(w, 422, -1, "请核对 USDT 通道、商户授权、费率和限额", nil)
	case errors.Is(err, cryptopay.ErrConflict):
		response(w, 409, -1, "通道配置已变化，请重新打开后确认", nil)
	case errors.Is(err, cryptopay.ErrNotFound):
		response(w, 404, -1, "USDT 通道不存在或已不可用", nil)
	default:
		response(w, 503, -1, "USDT 通道配置暂时无法核实，请查看原请求结果", nil)
	}
}

func (s *Server) reservedPaymentChannelIDs() []int64 {
	ids := []int64{}
	if s.ProviderFlow != nil {
		for id := range s.ProviderFlow.Providers {
			if id > 0 {
				ids = append(ids, id)
			}
		}
		for id := range s.ProviderFlow.ParentFactories {
			if id > 0 {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// cryptoChannelConsole is called only after the common parent channel role
// gate; the transaction also checks the original durable super session.
func (s *Server) cryptoChannelConsole(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string, token string) bool {
	if len(parts) < 3 || parts[2] != "crypto" {
		return false
	}
	if s.Pool == nil || s.Store == nil {
		cryptoChannelError(w, cryptopay.ErrUnavailable)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(parts) > 4 {
		response(w, 400, -1, "无效的 USDT 通道请求", nil)
		return true
	}
	id := int64(0)
	if len(parts) == 4 {
		var ok bool
		id, ok = positiveFundsID(parts[3])
		if !ok {
			response(w, 400, -1, "无效的 USDT 通道编号", nil)
			return true
		}
	}
	ctx := s.cryptoChannelContext(r.Context(), session, token)
	if r.Method == "GET" {
		rows, err := s.cryptoChannelList(ctx)
		if err != nil {
			cryptoChannelError(w, err)
			return true
		}
		items := make([]cryptoChannelView, 0, len(rows))
		for _, row := range rows {
			if s.ProviderFlow != nil && (s.ProviderFlow.Providers[row.ID] != nil || s.ProviderFlow.ParentFactories[row.ID] != nil) {
				cryptoChannelError(w, cryptopay.ErrConflict)
				return true
			}
			if id > 0 && row.ID == id {
				response(w, 200, 0, "", s.cryptoChannelView(row))
				return true
			}
			items = append(items, s.cryptoChannelView(row))
		}
		if id > 0 {
			cryptoChannelError(w, cryptopay.ErrNotFound)
		} else {
			response(w, 200, 0, "", map[string]any{"items": items})
		}
		return true
	}
	if r.Method != "POST" && r.Method != "PATCH" || r.Method == "POST" && id != 0 || r.Method == "PATCH" && id == 0 {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		response(w, 400, -1, "请使用 JSON 提交通道配置", nil)
		return true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		response(w, 400, -1, "通道配置过大或无效", nil)
		return true
	}
	in, err := decodeCryptoChannelInput(raw, id)
	if err != nil {
		response(w, 400, -1, "USDT 通道字段无效或不完整", nil)
		return true
	}
	in.ActorID = session.User.ID
	in.ReservedIDs = s.reservedPaymentChannelIDs()
	for _, reserved := range in.ReservedIDs {
		if id > 0 && id == reserved {
			cryptoChannelError(w, cryptopay.ErrConflict)
			return true
		}
	}
	row, err := (&cryptopay.Service{Pool: s.Pool}).SaveChannel(ctx, in)
	if err != nil {
		cryptoChannelError(w, err)
		return true
	}
	response(w, 200, 0, "", s.cryptoChannelView(row))
	return true
}

func decodeCryptoChannelInput(raw []byte, id int64) (cryptopay.SaveChannelInput, error) {
	out := cryptopay.SaveChannelInput{ID: id}
	fields, err := parentExactObject(raw)
	if err != nil {
		return out, err
	}
	required := []string{"name", "network", "enabled", "archived", "fee_bps", "min_amount", "max_amount", "day_limit", "merchant_ids", "expected_revision"}
	if id == 0 {
		required = append(required, "request_id")
	}
	if len(fields) != len(required) {
		return out, cryptopay.ErrInvalid
	}
	for _, key := range required {
		if fields[key] == nil {
			return out, cryptopay.ErrInvalid
		}
	}
	var wire struct {
		Name             string   `json:"name"`
		Network          string   `json:"network"`
		Enabled          bool     `json:"enabled"`
		Archived         bool     `json:"archived"`
		FeeBPS           int64    `json:"fee_bps"`
		MinAmount        string   `json:"min_amount"`
		MaxAmount        string   `json:"max_amount"`
		DayLimit         string   `json:"day_limit"`
		MerchantIDs      []string `json:"merchant_ids"`
		ExpectedRevision string   `json:"expected_revision"`
		RequestID        string   `json:"request_id"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return out, cryptopay.ErrInvalid
	}
	revision, err := strconv.ParseInt(wire.ExpectedRevision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != wire.ExpectedRevision || (id == 0) != (revision == 0) || wire.MerchantIDs == nil || len(wire.MerchantIDs) > 1000 {
		return out, cryptopay.ErrInvalid
	}
	out.ExpectedRevision = revision
	out.RequestID, out.Name, out.Network = wire.RequestID, wire.Name, wire.Network
	out.Enabled, out.Archived, out.FeeBPS = wire.Enabled, wire.Archived, wire.FeeBPS
	out.MinAmount, out.MaxAmount, out.DayLimit = wire.MinAmount, wire.MaxAmount, wire.DayLimit
	out.MerchantIDs = []int64{}
	seen := map[int64]bool{}
	for _, rawID := range wire.MerchantIDs {
		mid, ok := positiveFundsID(rawID)
		if !ok || seen[mid] {
			return out, cryptopay.ErrInvalid
		}
		seen[mid] = true
		out.MerchantIDs = append(out.MerchantIDs, mid)
	}
	return out, nil
}
