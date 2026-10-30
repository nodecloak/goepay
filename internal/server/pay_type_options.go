package server

import (
	"net/http"

	"goepay/internal/identity"
)

// payTypeOptions is the read-only 支付方式 option list the console filters and
// statistics pages use to build their 支付方式 dropdowns. The original console
// loads these from the live channel/pay-type list (chunk order c()/P(),
// value=type, label=showname) rather than hardcoding a fixed set; GOEPAY's
// router already accepts every enabled CatalogPayType, so this mirrors that by
// returning the enabled pay types with their shownames.
//
// It is a pure read of configuration both roles may see: it exposes only the
// type code and its public showname (never a credential, a channel secret or a
// merchant-scoped value), so admin and merchant alike may call it. GET only,
// no query parameters.
func (s *Server) payTypeOptions(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 2 || parts[1] != "pay-type-options" {
		return false
	}
	if role != "admin" && role != "merchant" {
		response(w, 403, -1, "authenticated console scope required", nil)
		return true
	}
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "pay type options take no parameters", nil)
		return true
	}
	// The effective enabled state of each type: a record enables/disables it,
	// otherwise the original pre_type default applies (alipay/wxpay open, the
	// rest closed). Records for non-catalog enabled types (ecny, …) surface too.
	enabled := map[string]bool{}
	for _, t := range originalPayTypes {
		enabled[t.Name] = t.Open
	}
	shownames := map[string]string{}
	for _, t := range originalPayTypes {
		shownames[t.Name] = t.ShowName
	}
	order := []string{}
	for _, t := range originalPayTypes {
		order = append(order, t.Name)
	}
	// Latest record per type wins (ORDER BY id DESC, first seen kept), matching
	// listPayTypes' merge. Only name+type+enabled are read.
	rows, e := s.Pool.Query(r.Context(), "SELECT body->>'type',body->>'name',body->'enabled'='true'::jsonb FROM backoffice_records WHERE resource='pay-types' ORDER BY id DESC")
	if e != nil {
		s.fail(w, e)
		return true
	}
	seenRecord := map[string]bool{}
	for rows.Next() {
		var kind, name string
		var on bool
		if e = rows.Scan(&kind, &name, &on); e != nil {
			rows.Close()
			s.fail(w, e)
			return true
		}
		if kind == "" {
			continue
		}
		if !seenRecord[kind] {
			seenRecord[kind] = true
			if _, original := enabled[kind]; !original {
				order = append(order, kind)
			}
			enabled[kind] = on
		}
		if name != "" {
			shownames[kind] = name
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		s.fail(w, e)
		return true
	}
	type option struct {
		Value    string `json:"value"`
		Showname string `json:"showname"`
	}
	options := []option{}
	for _, kind := range order {
		if !enabled[kind] {
			continue
		}
		label := shownames[kind]
		if label == "" {
			label = kind
		}
		options = append(options, option{Value: kind, Showname: label})
	}
	// The local-simulation pay type is selectable only while the sandbox is on,
	// exactly as the create path admits it; it has no pre_type record.
	if s.Sandbox {
		options = append(options, option{Value: "sandbox", Showname: "本地模拟"})
	}
	response(w, 200, 0, "", map[string]any{"items": options})
	return true
}
