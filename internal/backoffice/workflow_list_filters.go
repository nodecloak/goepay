package backoffice

import (
	"net/http"
	"net/url"
	"strconv"
)

// Original merchant links must not silently broaden to a platform-wide list.
// The authenticated merchant predicate remains authoritative for every filter.
func workflowListFilters(r *http.Request, p Principal) (page, limit int, merchant int64, err error) {
	page, limit, merchant = 1, 20, p.SubjectID
	if p.Role == "admin" {
		merchant = 0
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		err = ErrInvalid
		return
	}
	for key, values := range q {
		if len(values) != 1 || (key != "page" && key != "limit" && key != "merchant_id") {
			err = ErrInvalid
			return
		}
		n, e := strconv.ParseInt(values[0], 10, 64)
		if e != nil || n <= 0 || strconv.FormatInt(n, 10) != values[0] {
			err = ErrInvalid
			return
		}
		switch key {
		case "page":
			if n > 100000 {
				err = ErrInvalid
				return
			}
			page = int(n)
		case "limit":
			if n > 100 {
				err = ErrInvalid
				return
			}
			limit = int(n)
		case "merchant_id":
			if p.Role != "admin" && n != p.SubjectID {
				err = ErrForbidden
				return
			}
			merchant = n
		}
	}
	return
}
