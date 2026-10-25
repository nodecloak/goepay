package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Printing configuration is a complete CAS replacement. Explicit empty secrets
// plus keep/clear/replace prevent omitted fields from silently changing policy.
func printingBody(w http.ResponseWriter, r *http.Request, kind string, out any) bool {
	var shape map[string]string
	switch kind {
	case "platform":
		shape = map[string]string{"expected_revision": "id", "enabled": "bool", "provider": "text", "credential_action": "text", "app_id": "text", "secret": "text"}
	case "merchant":
		shape = map[string]string{"expected_revision": "id", "mode": "int", "copies": "int", "voice": "bool", "device_action": "text", "device_id": "text"}
	case "manual":
		shape = map[string]string{"external_no": "text", "previous_job_id": "id", "acknowledge_duplicate": "bool"}
	case "recover":
		shape = map[string]string{"expected_task_id": "id"}
	default:
		response(w, 400, -1, "unknown printing body", nil)
		return false
	}
	var fields map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &fields) {
		return false
	}
	valid := len(fields) == len(shape)
	for key, typ := range shape {
		raw, exists := fields[key]
		if !exists || string(raw) == "null" {
			valid = false
			continue
		}
		switch typ {
		case "text", "id":
			var v string
			if json.Unmarshal(raw, &v) != nil {
				valid = false
			} else if typ == "id" {
				n, e := strconv.ParseInt(v, 10, 64)
				valid = valid && e == nil && n >= 0 && strconv.FormatInt(n, 10) == v
				if kind == "recover" && n == 0 {
					valid = false
				}
			}
		case "bool":
			var v bool
			valid = json.Unmarshal(raw, &v) == nil && valid
		case "int":
			var v int
			valid = json.Unmarshal(raw, &v) == nil && valid
		}
	}
	raw, err := json.Marshal(fields)
	if !valid || err != nil || json.Unmarshal(raw, out) != nil {
		response(w, 422, -1, "explicit typed printing fields and canonical revisions required", nil)
		return false
	}
	return true
}
