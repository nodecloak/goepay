package channelconfig

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	adapter "goepay/internal/adapters/payment"
)

// DefaultPaymentChannels validates an explicit collection-only permission pool.
// It does not change the merchant admission used by transfers or payouts.
func (p ParentStartupConfiguration) DefaultPaymentChannels(raw string) ([]int64, error) {
	registered := make(map[int64]bool, len(p.origins))
	for _, c := range p.origins {
		registered[c.ID] = true
	}
	return defaultPaymentChannelsRegistered236(raw, func(id int64) bool { return registered[id] })
}

// DefaultPaymentChannels validates the same collection-only pool against a
// constructed provider registry, including the local simulation registry.
func DefaultPaymentChannels(raw string, providers map[int64]adapter.Provider) ([]int64, error) {
	return defaultPaymentChannelsRegistered236(raw, func(id int64) bool {
		_, ok := providers[id]
		return ok
	})
}

func defaultPaymentChannelsRegistered236(raw string, registered func(int64) bool) ([]int64, error) {
	out := []int64{}
	if raw == "" {
		return out, nil
	}
	if len(raw) > 64<<10 {
		return nil, ErrImport
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return nil, ErrImport
	}
	seen := map[int64]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, ErrImport
		}
		number, ok := token.(json.Number)
		if !ok {
			return nil, ErrImport
		}
		id, err := number.Int64()
		if err != nil || id <= 0 || !registered(id) || seen[id] || len(out) >= 64 {
			return nil, ErrImport
		}
		seen[id] = true
		out = append(out, id)
	}
	if token, err = d.Token(); err != nil || token != json.Delim(']') {
		return nil, ErrImport
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrImport
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
