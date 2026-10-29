package backoffice

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// BatchDomainStatus sets the 审核通过 (approved) flag on each selected 授权域名 by
// running the resource's existing single Update — so every row goes through the
// same admin-only field guard, validation and domain-review audit the one-row
// 通过/拒绝 edit does. It stops on the first failure and reports how many changed.
// The original admin 授权域名 page had a 批量操作 dropdown (通过/拒绝/删除); delete is
// already covered by batch-delete, this adds approve/reject.
func (s *Service) BatchDomainStatus(ctx context.Context, p Principal, ids []int64, approved bool) (int, error) {
	if p.Role != "admin" {
		return 0, ErrForbidden
	}
	if len(ids) == 0 || len(ids) > 200 {
		return 0, ErrInvalid
	}
	body := map[string]json.RawMessage{"approved": json.RawMessage(strconv.FormatBool(approved))}
	done := 0
	for _, id := range ids {
		if _, e := s.Update(ctx, p, "domains", id, body); e != nil {
			return done, e
		}
		done++
	}
	return done, nil
}

// serveBatchDomainStatus handles POST /resources/domains/batch-status
// {"ids":[...],"approved":true|false}.
func (s *Service) serveBatchDomainStatus(w http.ResponseWriter, r *http.Request, p Principal, resource string) {
	if resource != "domains" {
		write(w, 404, nil, "not found")
		return
	}
	var in struct {
		IDs      []json.RawMessage `json:"ids"`
		Approved *bool             `json:"approved"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(&in); e != nil || in.Approved == nil {
		write(w, 400, nil, "invalid JSON")
		return
	}
	ids := make([]int64, 0, len(in.IDs))
	for _, m := range in.IDs {
		id, e := strconv.ParseInt(strings.Trim(string(m), `"`), 10, 64)
		if e != nil || id <= 0 {
			write(w, 400, nil, "invalid ID")
			return
		}
		ids = append(ids, id)
	}
	done, e := s.BatchDomainStatus(r.Context(), p, ids, *in.Approved)
	if e != nil {
		writeError(w, e)
		return
	}
	write(w, 200, map[string]any{"updated": done}, "")
}
