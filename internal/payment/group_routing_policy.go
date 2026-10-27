package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"goepay/internal/merchantpolicy"
	"strconv"
	"time"
)

type GroupPaymentRouting struct {
	GroupID  int64
	Choice   grouppolicy.Choice
	Snapshot json.RawMessage
	// The membership row is held FOR SHARE through route commit. The caller
	// must check this immutable deadline after waiting for downstream quotas.
	ValidUntil time.Time
}

func (s *Store) GroupPaymentRoutingTx(ctx context.Context, tx pgx.Tx, mid int64, kind string) (GroupPaymentRouting, error) {
	var out GroupPaymentRouting
	if tx == nil || mid <= 0 || kind != "alipay" && kind != "wxpay" {
		return out, ErrInvalid
	}
	g, e := merchantpolicy.ReadEffectiveGroup(ctx, tx, mid, true)
	if e != nil {
		if errors.Is(e, merchantpolicy.ErrGroupPolicy) {
			return out, ErrState
		}
		return out, e
	}
	out.GroupID, out.ValidUntil = g.ID, g.ValidUntil
	body := g.Body
	var fields struct {
		Enabled *bool           `json:"enabled"`
		Info    json.RawMessage `json:"info"`
	}
	if json.Unmarshal(body, &fields) != nil || fields.Enabled != nil && !*fields.Enabled {
		return out, ErrState
	}
	if len(fields.Info) == 0 {
		fields.Info = json.RawMessage(`{}`)
	}
	info, e := grouppolicy.ParseInfo(fields.Info)
	if e != nil {
		return out, ErrInvalid
	}
	out.Choice = info[kind].RoutingChoice()
	canonical, e := info.CanonicalJSON()
	if e != nil {
		return out, ErrInvalid
	}
	out.Snapshot, e = json.Marshal(struct {
		GroupID string          `json:"group_id"`
		Info    json.RawMessage `json:"payment_info"`
	}{strconv.FormatInt(out.GroupID, 10), canonical})
	return out, e
}
