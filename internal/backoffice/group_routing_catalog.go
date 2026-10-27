package backoffice

import (
	"net/http"
	"sort"
	"strconv"
)

type GroupPaymentChannel struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Methods []string `json:"methods"`
}

func (s *Service) serveGroupRoutingCatalog(w http.ResponseWriter, r *http.Request, p Principal, parts []string) bool {
	if len(parts) != 2 || parts[0] != "groups" || parts[1] != "routing-options" {
		return false
	}
	if p.Role != "admin" || p.SubjectID <= 0 {
		writeError(w, ErrForbidden)
		return true
	}
	if r.Method != "GET" {
		write(w, 405, nil, "GET required")
		return true
	}
	if r.URL.RawQuery != "" {
		write(w, 400, nil, "routing catalog accepts no filters")
		return true
	}
	items := []GroupPaymentChannel{}
	if s.GroupRoutingChannels != nil {
		var e error
		items, e = s.GroupRoutingChannels()
		if e != nil {
			writeError(w, e)
			return true
		}
		if len(items) > 1000 {
			writeError(w, ErrInvalid)
			return true
		}
		if items == nil {
			items = []GroupPaymentChannel{}
		}
		ids := map[int64]bool{}
		for _, item := range items {
			id, e := strconv.ParseInt(item.ID, 10, 64)
			if e != nil || id <= 0 || strconv.FormatInt(id, 10) != item.ID || ids[id] || (item.Type != "alipay" && item.Type != "wxpay") || len(item.Methods) == 0 || len(item.Methods) > 32 {
				writeError(w, ErrInvalid)
				return true
			}
			ids[id] = true
			methods := map[string]bool{}
			for _, method := range item.Methods {
				switch method {
				case "page", "wap", "native", "h5", "app", "jsapi", "mini", "applet", "scan", "preauth":
				default:
					writeError(w, ErrInvalid)
					return true
				}
				if methods[method] {
					writeError(w, ErrInvalid)
					return true
				}
				methods[method] = true
			}
		}
		sort.Slice(items, func(i, j int) bool {
			a, _ := strconv.ParseInt(items[i].ID, 10, 64)
			b, _ := strconv.ParseInt(items[j].ID, 10, 64)
			return a < b
		})
	}
	write(w, 200, map[string]any{"items": items, "available": s.GroupRoutingChannels != nil}, "")
	return true
}
