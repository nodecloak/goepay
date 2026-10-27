package server

import "net/http"

// Only these two intended public layout positions are exposed. Merchant welcome
// content is read inside the authenticated dashboard's original statement.
func (s *Server) publicAnnouncementLayout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "layout query parameters not accepted", nil)
		return
	}
	var registration, footer string
	if e := s.Pool.QueryRow(r.Context(), `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='zhuce'),''),COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='footer'),'')`).Scan(&registration, &footer); e != nil {
		s.fail(w, e)
		return
	}
	if len(registration) > 20000 || len(footer) > 20000 {
		response(w, 503, -1, "layout unavailable", nil)
		return
	}
	response(w, 200, 0, "ok", map[string]string{"zhuce": registration, "footer": footer, "format": "html"})
}
