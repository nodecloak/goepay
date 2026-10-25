package server

import (
	"net/url"
	"strconv"
)

// A misspelled or unsupported original filter must not silently become an
// unrestricted list/export. This validation augments the independent tenant
// predicate and leaves the existing filters for other resources unchanged.
func validateTransferListFilters(q url.Values) error {
	allowed := map[string]bool{"page": true, "limit": true, "q": true, "search_field": true, "format": true, "merchant_id": true, "channel_id": true, "status": true, "from": true, "to": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 {
			return errListFilter
		}
	}
	for _, key := range []string{"merchant_id", "channel_id"} {
		if raw := q.Get(key); raw != "" {
			value, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || value <= 0 || strconv.FormatInt(value, 10) != raw {
				return errListFilter
			}
		}
	}
	for key, maximum := range map[string]int64{"page": 1000000, "limit": 100} {
		if values, ok := q[key]; ok {
			value, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || value <= 0 || value > maximum || strconv.FormatInt(value, 10) != values[0] {
				return errListFilter
			}
		}
	}
	if values, ok := q["format"]; ok && values[0] != "csv" {
		return errListFilter
	}
	return nil
}
