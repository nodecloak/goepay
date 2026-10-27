package channelconfig

import (
	"bytes"
	"encoding/json"
	"io"
)

// WithParentStartupAccounts consumes only Store-authenticated plaintext account
// snapshots supplied by trusted startup wiring, never an HTTP request.
func WithParentStartupAccounts(imp Import, snapshots map[int64][]byte) (Import, error) {
	if imp.Version != 1 {
		return Import{}, ErrImport
	}
	out := imp
	out.Channels = make([]Channel, len(imp.Channels))
	seen := map[int64]bool{}
	for i, original := range imp.Channels {
		if original.ID <= 0 || seen[original.ID] {
			return Import{}, ErrImport
		}
		seen[original.ID] = true
		out.Channels[i] = cloneParentChannel(original)
		raw, ok := snapshots[original.ID]
		if !ok {
			continue
		}
		var snapshot parentAccountSnapshot
		if len(raw) == 0 || len(raw) > 1048576 {
			return Import{}, ErrImport
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(&snapshot) != nil || snapshot.Version != 1 || snapshot.Channel.ID != original.ID || snapshot.Channel.Plugin != original.Plugin {
			return Import{}, ErrImport
		}
		if _, e := d.Token(); e != io.EOF {
			return Import{}, ErrImport
		}
		// Constructor validation is performed immediately by the configured
		// factory; replacing metadata never creates a new authorization origin.
		out.Channels[i] = snapshot.Channel
	}
	for id := range snapshots {
		if !seen[id] {
			return Import{}, ErrImport
		}
	}
	return out, nil
}
