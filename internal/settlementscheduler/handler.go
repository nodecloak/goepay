package settlementscheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"goepay/internal/platform/jobs"
)

const GenerateKind = "settlement-plan.generate"

type Generator interface {
	GenerateSettlementPlanRun(context.Context, int64) error
}
type Handler struct{ Store Generator }

var ErrPayload = errors.New("invalid settlement generation job")

func (h Handler) Handle(ctx context.Context, j jobs.Job) error {
	if j.Kind != GenerateKind || len(j.Payload) == 0 || len(j.Payload) > 1024 {
		return ErrPayload
	}
	d := json.NewDecoder(bytes.NewReader(j.Payload))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return ErrPayload
	}
	key, e := d.Token()
	if e != nil || key != "run_id" {
		return ErrPayload
	}
	value, e := d.Token()
	text, ok := value.(string)
	if e != nil || !ok {
		return ErrPayload
	}
	id, e := strconv.ParseInt(text, 10, 64)
	if e != nil || id < 1 || strconv.FormatInt(id, 10) != text {
		return ErrPayload
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') {
		return ErrPayload
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrPayload
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if h.Store == nil {
		return ErrUnavailable
	}
	if gate, ok := h.Store.(interface {
		VerifyScheduledJobClaim(context.Context, jobs.Job) error
	}); ok {
		if e := gate.VerifyScheduledJobClaim(ctx, j); e != nil {
			return e
		}
	}
	return h.Store.GenerateSettlementPlanRun(ctx, id)
}
