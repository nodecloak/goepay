package backoffice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

type PayRollChannelSnapshot struct {
	ID       string         `json:"id"`
	Revision string         `json:"revision"`
	Fields   map[string]any `json:"fields"`
}

func splitPayRollChannelIDs(ids string) []string {
	if ids == "" {
		return nil
	}
	result := []string{}
	for _, entry := range strings.Split(ids, ",") {
		result = append(result, strings.Split(strings.TrimSpace(entry), ":")[0])
	}
	return result
}

func payRollSnapshot(id int64, raw []byte) PayRollChannelSnapshot {
	hash := sha256.Sum256(raw)
	return PayRollChannelSnapshot{ID: strconv.FormatInt(id, 10), Revision: hex.EncodeToString(hash[:]), Fields: decodeFields(raw)}
}

func (s *Service) PayRollChannelSnapshot(ctx context.Context, p Principal, id int64) (PayRollChannelSnapshot, error) {
	if _, e := definitionFor(p, "pay-rolls", false); e != nil {
		return PayRollChannelSnapshot{}, e
	}
	if id <= 0 {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	var raw []byte
	if e := s.pool.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-rolls' AND id=$1`, id).Scan(&raw); e != nil {
		return PayRollChannelSnapshot{}, businessError(e)
	}
	return payRollSnapshot(id, raw), nil
}

func (s *Service) UpdatePayRollChannels(ctx context.Context, p Principal, id int64, in map[string]json.RawMessage) (PayRollChannelSnapshot, error) {
	if _, e := definitionFor(p, "pay-rolls", true); e != nil {
		return PayRollChannelSnapshot{}, e
	}
	if id <= 0 {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	for key := range in {
		if key != "expected_revision" && key != "channel_ids" && key != "confirm_enabled" {
			return PayRollChannelSnapshot{}, ErrInvalid
		}
	}
	expected, e := stringInput(in, "expected_revision")
	if e != nil || len(expected) != 64 {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	if decoded, e := hex.DecodeString(expected); e != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != expected {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	ids, e := stringInput(in, "channel_ids")
	if e != nil || in["channel_ids"] == nil {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	confirmed := false
	if value, ok := in["confirm_enabled"]; ok && json.Unmarshal(value, &confirmed) != nil {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return PayRollChannelSnapshot{}, e
	}
	defer tx.Rollback(ctx)
	var raw []byte
	if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-rolls' AND id=$1 FOR UPDATE`, id).Scan(&raw); e != nil {
		return PayRollChannelSnapshot{}, businessError(e)
	}
	before := payRollSnapshot(id, raw)
	if before.Revision != expected {
		return PayRollChannelSnapshot{}, ErrConflict
	}
	v := before.Fields
	if v["enabled"] == true && v["channel_ids"] != ids && !confirmed {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	v["channel_ids"] = ids
	// Remove only rules whose channels were explicitly removed. All surviving
	// rule tokens and all unrelated policy fields remain the stored values.
	rules, e := routeJSONObject(v["rules"])
	if e != nil {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	selected := map[string]bool{}
	for _, entry := range splitPayRollChannelIDs(ids) {
		selected[entry] = true
	}
	pruned := false
	for key := range rules {
		if !selected[key] {
			delete(rules, key)
			pruned = true
		}
	}
	if pruned {
		encoded, e := json.Marshal(rules)
		if e != nil {
			return PayRollChannelSnapshot{}, e
		}
		v["rules"] = string(encoded)
	}
	roll, e := ParsePayRoll(v)
	if e != nil {
		return PayRollChannelSnapshot{}, e
	}
	if s.GroupChannelAllowed == nil && len(roll.Channels) > 0 {
		return PayRollChannelSnapshot{}, ErrInvalid
	}
	for _, channel := range roll.Channels {
		if !s.GroupChannelAllowed(channel.ID, roll.Type) {
			return PayRollChannelSnapshot{}, ErrInvalid
		}
	}
	encoded, e := json.Marshal(v)
	if e != nil {
		return PayRollChannelSnapshot{}, e
	}
	// RETURNING uses PostgreSQL's canonical JSONB text, the same bytes as GET.
	if e = tx.QueryRow(ctx, `UPDATE backoffice_records SET body=$2::jsonb,updated_at=now() WHERE resource='pay-rolls' AND id=$1 RETURNING body`, id, string(encoded)).Scan(&raw); e != nil {
		return PayRollChannelSnapshot{}, businessError(e)
	}
	if e = audit(ctx, tx, p, "pay-rolls", id, "configure-channels", map[string]any{"channel_ids": ids, "confirm_enabled": confirmed, "expected_revision": expected}); e != nil {
		return PayRollChannelSnapshot{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return PayRollChannelSnapshot{}, businessError(e)
	}
	return payRollSnapshot(id, raw), nil
}
