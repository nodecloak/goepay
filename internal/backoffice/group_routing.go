package backoffice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
)

func normalizeGroupPaymentInfo(in map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		out[key] = append(json.RawMessage(nil), value...)
	}
	if raw, exists := out["info"]; exists {
		var encoded string
		if json.Unmarshal(raw, &encoded) == nil {
			raw = json.RawMessage(encoded)
		}
		info, e := grouppolicy.ParseInfo(raw)
		if e != nil {
			return nil, ErrInvalid
		}
		canonical, e := info.CanonicalJSON()
		if e != nil {
			return nil, ErrInvalid
		}
		out["info"] = canonical
	}
	return out, nil
}

func decodeResourceFields(resource string, raw []byte) (map[string]any, error) {
	fields := decodeFields(raw)
	if resource != "groups" {
		return fields, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil, ErrInvalid
	}
	normalized, e := normalizeGroupPaymentInfo(object)
	if e != nil {
		// Preserve unsupported historical settings for inspection, including
		// exact numeric tokens. A single legacy group must not hide the list.
		// New writes still pass the strict normalizer and reference checks.
		if original, exists := object["info"]; exists {
			var encoded string
			if json.Unmarshal(original, &encoded) == nil {
				fields["info"] = encoded
			} else {
				fields["info"] = string(original)
			}
		}
		return fields, nil
	}
	if info, exists := normalized["info"]; exists {
		var value map[string]any
		if json.Unmarshal(info, &value) != nil {
			return nil, ErrInvalid
		}
		fields["info"] = value
	}
	return fields, nil
}

func (s *Service) validateGroupRoutingReferences(ctx context.Context, tx pgx.Tx, fields map[string]any) error {
	if fields["info"] == nil {
		return nil
	}
	raw, e := json.Marshal(fields["info"])
	if e != nil {
		return ErrInvalid
	}
	info, e := grouppolicy.ParseInfo(raw)
	if e != nil {
		return ErrInvalid
	}
	for kind, policy := range info {
		choice := policy.RoutingChoice()
		switch choice.Kind() {
		case grouppolicy.Subchannels, grouppolicy.Inherit, grouppolicy.Disabled, grouppolicy.Random, grouppolicy.RandomRoll, grouppolicy.Sequential, grouppolicy.First:
		case grouppolicy.Channel:
			if s.GroupChannelAllowed == nil || !s.GroupChannelAllowed(choice.ID(), kind) {
				return ErrInvalid
			}
		case grouppolicy.Roll:
			var body []byte
			if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-rolls' AND id=$1 FOR SHARE`, choice.ID()).Scan(&body); e != nil {
				if e == pgx.ErrNoRows {
					return ErrInvalid
				}
				return e
			}
			var value map[string]any
			if json.Unmarshal(body, &value) != nil {
				return ErrInvalid
			}
			roll, e := ParsePayRoll(value)
			// A group can cover multiple merchants. A merchant-specific roll is
			// not a transferable group permission; group choices use global rolls.
			if e != nil || !roll.Enabled || roll.Type != kind || roll.MerchantID != 0 || s.GroupChannelAllowed == nil {
				return ErrInvalid
			}
			for _, channel := range roll.Channels {
				if !s.GroupChannelAllowed(channel.ID, kind) {
					return ErrInvalid
				}
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}
