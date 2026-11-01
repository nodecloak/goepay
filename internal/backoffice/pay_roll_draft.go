package backoffice

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// ServePayRollEditor runs behind the same session, node grant and CSRF checks
// as Serve. It independently applies the resource's read/write permission.
func (s *Service) ServePayRollEditor(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) < 2 || parts[0] != "pay-rolls" || !(len(parts) == 2 && (parts[1] == "drafts" || parts[1] == "routing-options") || len(parts) == 3 && parts[2] == "channels") {
		return false
	}
	writeRequest := r.Method != "GET"
	if _, e := definitionFor(p, "pay-rolls", writeRequest); e != nil {
		writeError(w, e)
		return true
	}
	if r.URL.RawQuery != "" {
		writeError(w, ErrInvalid)
		return true
	}
	if len(parts) == 2 && parts[1] == "routing-options" {
		return s.serveGroupRoutingCatalog(w, r, p, []string{"groups", "routing-options"})
	}
	if len(parts) == 2 {
		if r.Method != "POST" {
			write(w, 405, nil, "POST required")
			return true
		}
		in, ok := body(w, r)
		if !ok {
			return true
		}
		item, e := s.CreatePayRollDraft(r.Context(), p, in)
		if e != nil {
			writeError(w, e)
		} else {
			write(w, 201, flatten(item), "")
		}
		return true
	}
	id, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[1] {
		writeError(w, ErrInvalid)
		return true
	}
	switch r.Method {
	case "GET":
		result, e := s.PayRollChannelSnapshot(r.Context(), p, id)
		if e != nil {
			writeError(w, e)
		} else {
			write(w, 200, result, "")
		}
	case "POST":
		in, ok := body(w, r)
		if !ok {
			return true
		}
		result, e := s.UpdatePayRollChannels(r.Context(), p, id, in)
		if e != nil {
			writeError(w, e)
		} else {
			write(w, 200, result, "")
		}
	default:
		write(w, 405, nil, "GET or POST required")
	}
	return true
}

func (s *Service) CreatePayRollDraft(ctx context.Context, p Principal, in map[string]json.RawMessage) (Item, error) {
	d, e := definitionFor(p, "pay-rolls", true)
	if e != nil {
		return Item{}, e
	}
	for key := range in {
		if key != "creation_mode" && key != "name" && key != "type" && key != "strategy" {
			return Item{}, ErrInvalid
		}
	}
	mode, e := stringInput(in, "creation_mode")
	if e != nil || mode != "draft" {
		return Item{}, ErrInvalid
	}
	strategy, e := stringInput(in, "strategy")
	if e != nil || strategy != "round_robin" && strategy != "weighted" && strategy != "first" {
		return Item{}, ErrInvalid
	}
	complete := map[string]json.RawMessage{}
	for k, v := range in {
		if k != "creation_mode" {
			complete[k] = v
		}
	}
	for k, v := range map[string]any{"merchant_id": "0", "enabled": false, "default_route": false, "channel_ids": "", "limits": "{}", "rules": "{}"} {
		complete[k], _ = json.Marshal(v)
	}
	// The channel field remains required for legacy generic creation. Only this
	// dedicated path supplies and accepts the constrained empty draft.
	d.fields = append([]Field(nil), d.fields...)
	for i := range d.fields {
		if d.fields[i].Name == "channel_ids" {
			d.fields[i].Required = false
		}
	}
	v, e := validate(p, d, complete, nil, true)
	if e != nil {
		return Item{}, e
	}
	if _, e = ParsePayRoll(v); e != nil {
		return Item{}, e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return Item{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Item{}, e
	}
	defer tx.Rollback(ctx)
	var id int64
	if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',$1::jsonb) RETURNING id`, string(raw)).Scan(&id); e != nil {
		return Item{}, businessError(e)
	}
	if e = audit(ctx, tx, p, "pay-rolls", id, "create", v); e != nil {
		return Item{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Item{}, businessError(e)
	}
	return Item{ID: id, Fields: v}, nil
}
