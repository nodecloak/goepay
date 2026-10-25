package merchantnotice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
)

func (s *Store) wechatTemplateSettings(ctx context.Context, tx pgx.Tx, scene string) (WechatTemplateConfig, error) {
	switch scene {
	case "order", "settle", "login", "complain", "balance", "wxtransfer":
	default:
		return WechatTemplateConfig{}, ErrInvalid
	}
	base := "wxnotice_tpl_" + scene
	rows, err := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND (body->>'key'=$1 OR starts_with(body->>'key',$1||'_')) ORDER BY id`, base)
	if err != nil {
		return WechatTemplateConfig{}, err
	}
	defer rows.Close()
	out := WechatTemplateConfig{Fields: map[string]string{}}
	seen := map[string]bool{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if rows.Scan(&key, &raw) != nil || json.Unmarshal(raw, &value) != nil || len(value) > 128 {
			return WechatTemplateConfig{}, ErrState
		}
		if seen[key] {
			return WechatTemplateConfig{}, ErrConflict
		}
		seen[key] = true
		if key == base {
			out.ID = value
		} else if strings.HasPrefix(key, base+"_") && value != "" {
			out.Fields[key] = value
		}
	}
	if err = rows.Err(); err != nil {
		return WechatTemplateConfig{}, err
	}
	if out.ID == "" {
		return WechatTemplateConfig{}, ErrState
	}
	return out, nil
}
