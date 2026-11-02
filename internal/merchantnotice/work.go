package merchantnotice

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) globalSwitches(ctx context.Context, scene string) (GlobalSwitches, error) {
	switch scene {
	case "order", "settle", "login", "complain", "mchrisk", "balance", "group", "domain_audit", "daily_report":
	default:
		return GlobalSwitches{}, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id`, []string{"msgconfig_" + scene, "telegramnotice", "qqbotnotice", "voicenotice", "wxnotice", "mailnotice", "robotnotice", "smsnotice"})
	if err != nil {
		return GlobalSwitches{}, err
	}
	defer rows.Close()
	values := map[string]bool{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if rows.Scan(&key, &raw) != nil || json.Unmarshal(raw, &value) != nil || value != "0" && value != "1" {
			return GlobalSwitches{}, ErrState
		}
		if _, exists := values[key]; exists {
			return GlobalSwitches{}, ErrConflict
		}
		values[key] = value == "1"
	}
	if err = rows.Err(); err != nil {
		return GlobalSwitches{}, err
	}
	channels := noticeChannels230(values)
	voice := values["voicenotice"]
	if s.voice != nil {
		view, _, managed, e := s.voice.Resolve(ctx)
		if e != nil {
			return GlobalSwitches{}, e
		}
		if managed {
			voice = view.Enabled
		}
	}
	return GlobalSwitches{Channels: channels, MailRobot: values["msgconfig_"+scene], Telegram: values["telegramnotice"], QQBot: values["qqbotnotice"], Voice: voice, GroupSMS: true}, nil
}

// HandleJob uses only the routes sealed at the original business event. Each
// route has a permanent claim; replaying a partially completed job never sends
// a consumed route again, including when its remote result remains unknown.
func (s *Store) HandleJob(ctx context.Context, jobID int64, lease string, resolver Resolver) error {
	if !s.available() || ctx == nil || jobID <= 0 || len(lease) != 32 || resolver == nil {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(lease); err != nil {
		return ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var event int64
	if err = tx.QueryRow(ctx, `SELECT e.id FROM jobs j JOIN merchant_notice_events e ON j.id=e.job_id AND j.event_key='merchant-notice:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) WHERE j.id=$1 AND j.kind='merchant.notice' AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp()`, jobID, lease).Scan(&event); err != nil {
		return ErrState
	}
	_, data, err := s.readEvent(ctx, tx, event)
	if err != nil {
		return err
	}
	if len(data.Routes) == 0 {
		return ErrState
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	switches, err := s.globalSwitches(ctx, data.Input.Scene)
	if err != nil {
		return err
	}
	var failures []error
	for _, kind := range data.Routes {
		if err = s.Deliver(ctx, jobID, lease, kind, switches, resolver); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
