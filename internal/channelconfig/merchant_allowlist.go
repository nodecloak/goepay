package channelconfig

import (
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"io"
	"strconv"
	"strings"
)

// MerchantAllowlist validates the explicit merchant/channel admission shared by
// the API and the independent automatic payout executor. It performs no I/O.
func MerchantAllowlist(raw string, providers map[int64]adapter.Provider) (map[int64][]int64, error) {
	return merchantAllowlistRegistered(raw, func(id int64) bool { _, ok := providers[id]; return ok })
}
func merchantAllowlistRegistered(raw string, registered func(int64) bool) (map[int64][]int64, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, ErrImport
	}
	d := json.NewDecoder(strings.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, ErrImport
	}
	result := map[int64][]int64{}
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return nil, ErrImport
		}
		key, ok := token.(string)
		if !ok {
			return nil, ErrImport
		}
		id, e := strconv.ParseInt(key, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != key {
			return nil, ErrImport
		}
		if _, exists := result[id]; exists {
			return nil, ErrImport
		}
		var channels []int64
		if e := d.Decode(&channels); e != nil || channels == nil {
			return nil, ErrImport
		}
		seen := map[int64]bool{}
		for _, channel := range channels {
			if channel <= 0 || !registered(channel) || seen[channel] {
				return nil, ErrImport
			}
			seen[channel] = true
		}
		result[id] = channels
	}
	if token, e := d.Token(); e != nil || token != json.Delim('}') {
		return nil, ErrImport
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, ErrImport
	}
	return result, nil
}
