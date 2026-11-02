package merchantnotice

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) wechatTemplateReadiness230(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	for _, scene := range []string{"order", "settle", "login", "complain", "balance", "wxtransfer"} {
		cfg, e := s.wechatTemplateSettings(ctx, tx, scene)
		if e != nil {
			if errors.Is(e, ErrState) || errors.Is(e, ErrInvalid) || errors.Is(e, ErrConflict) {
				out[scene] = false
				continue
			}
			return nil, e
		}
		used := map[string]bool{}
		valid := cfg.ID != "" && len(cfg.Fields) > 0
		if known, e := ValidatePlatformSetting("wxnotice_tpl_"+scene, cfg.ID); !known || e != nil {
			valid = false
		}
		for key, value := range cfg.Fields {
			known, e := ValidatePlatformSetting(key, value)
			if !known || e != nil || !templateField80.MatchString(value) || used[value] {
				valid = false
			}
			used[value] = true
		}
		out[scene] = valid
	}
	return out, tx.Commit(ctx)
}
