package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func scheduledObject(d *json.Decoder, names ...string) (map[string]json.RawMessage, error) {
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, payment.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return nil, payment.ErrInvalid
		}
		key, ok := token.(string)
		if !ok {
			return nil, payment.ErrInvalid
		}
		allowed := false
		for _, name := range names {
			if name == key {
				allowed = true
			}
		}
		if !allowed || out[key] != nil {
			return nil, payment.ErrInvalid
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil || string(value) == "null" {
			return nil, payment.ErrInvalid
		}
		out[key] = value
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') || len(out) != len(names) {
		return nil, payment.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, payment.ErrInvalid
	}
	return out, nil
}
func scheduledRequest(w http.ResponseWriter, r *http.Request, key string, manual bool) (payment.ScheduledTaskInput, bool) {
	var in payment.ScheduledTaskInput
	fail := func() (payment.ScheduledTaskInput, bool) {
		response(w, 400, -1, "invalid scheduled task JSON", nil)
		return in, false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	names := []string{"enabled", "schedule", "params", "expected_fingerprint"}
	if manual {
		names = []string{"expected_fingerprint"}
	}
	raw, e := scheduledObject(json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)), names...)
	if e != nil {
		return fail()
	}
	if json.Unmarshal(raw["expected_fingerprint"], &in.ExpectedFingerprint) != nil || !identityHex(in.ExpectedFingerprint) {
		return fail()
	}
	if manual {
		return in, true
	}
	if json.Unmarshal(raw["enabled"], &in.Enabled) != nil {
		return fail()
	}
	schedule, e := scheduledObject(json.NewDecoder(strings.NewReader(string(raw["schedule"]))), "frequency", "interval", "execute_at")
	if e != nil {
		return fail()
	}
	if json.Unmarshal(schedule["frequency"], &in.Schedule.Frequency) != nil || json.Unmarshal(schedule["interval"], &in.Schedule.Interval) != nil || json.Unmarshal(schedule["execute_at"], &in.Schedule.ExecuteAt) != nil {
		return fail()
	}
	definitions, _ := payment.ScheduledTaskParameters(key)
	paramNames := []string{}
	for _, parameter := range definitions {
		paramNames = append(paramNames, parameter.Name)
	}
	params, e := scheduledObject(json.NewDecoder(strings.NewReader(string(raw["params"]))), paramNames...)
	if e != nil {
		return fail()
	}
	in.Params = payment.ScheduledTaskParams{}
	for name, raw := range params {
		var value int
		if json.Unmarshal(raw, &value) != nil {
			return fail()
		}
		in.Params[name] = value
	}
	return in, true
}
func (s *Server) scheduledTaskError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession):
		response(w, 403, -1, "original super administrator session required", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "registered scheduled task unavailable", nil)
	case errors.Is(e, payment.ErrConflict), errors.Is(e, payment.ErrState):
		response(w, 409, -1, "scheduled task changed or is busy; refresh before confirming", nil)
	case errors.Is(e, payment.ErrInvalid):
		response(w, 422, -1, "invalid schedule or unavailable task executor", nil)
	default:
		s.recordRuntimeFailure(e)
		response(w, 503, -1, "scheduled task could not be confirmed", nil)
	}
}
func (s *Server) scheduledTasks(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "scheduled-tasks" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if s.Store == nil || len(parts) > 4 {
		response(w, 503, -1, "scheduled task registry unavailable", nil)
		return true
	}
	a := payment.MerchantCredentialActor{User: session.User, SessionToken: token, Delegated: session.Issuer != nil}
	key := ""
	if len(parts) >= 3 {
		key = parts[2]
	}
	history := len(parts) == 4 && parts[3] == "runs" && r.Method == "GET"
	if !history && (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if history {
		q := r.URL.Query()
		limit, before := 20, int64(0)
		for k, v := range q {
			if len(v) != 1 || (k != "limit" && k != "before_id") {
				response(w, 400, -1, "invalid task history pagination", nil)
				return true
			}
			n, e := strconv.ParseInt(v[0], 10, 64)
			if e != nil || strconv.FormatInt(n, 10) != v[0] || n < 0 || (k == "limit" && (n < 1 || n > 100)) {
				response(w, 400, -1, "invalid task history pagination", nil)
				return true
			}
			if k == "limit" {
				limit = int(n)
			} else {
				before = n
			}
		}
		out, e := s.Store.ScheduledTaskRuns(r.Context(), a, key, before, limit)
		if e != nil {
			s.scheduledTaskError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}
	if len(parts) <= 3 && r.Method == "GET" {
		items, e := s.Store.ScheduledTasks(r.Context(), a, key)
		if e != nil {
			s.scheduledTaskError(w, e)
		} else if key == "" {
			response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
		} else {
			response(w, 200, 0, "", items[0])
		}
		return true
	}
	if len(parts) == 3 && r.Method == "PATCH" {
		in, ok := scheduledRequest(w, r, key, false)
		if !ok {
			return true
		}
		out, e := s.Store.UpdateScheduledTask(r.Context(), a, key, in)
		if e != nil {
			s.scheduledTaskError(w, e)
		} else {
			response(w, 200, 0, "scheduled task configuration saved", out)
		}
		return true
	}
	if len(parts) == 4 && parts[3] == "run" && r.Method == "POST" {
		in, ok := scheduledRequest(w, r, key, true)
		if !ok {
			return true
		}
		out, e := s.Store.RequestScheduledTaskRun(r.Context(), a, key, in.ExpectedFingerprint)
		if e != nil {
			s.scheduledTaskError(w, e)
		} else {
			response(w, 200, 0, "scheduled only; no execution result yet", out)
		}
		return true
	}
	response(w, 405, -1, "scheduled task operation unavailable", nil)
	return true
}
