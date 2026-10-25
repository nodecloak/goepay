package server

import (
	"context"
	"goepay/internal/identity"
	"log/slog"
)

func (s *Server) recordVerifiedMerchantLogin(ctx context.Context, user identity.User, ip string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES($1,'merchant',$2,true,$3,$4,pg_current_xact_id()) RETURNING id`, user.Username, ip, user.ID, user.AuthVersion).Scan(&id); err != nil {
		return err
	}
	if s.MerchantNotice != nil {
		child, noticeErr := tx.Begin(ctx)
		if noticeErr == nil {
			noticeErr = s.MerchantNotice.CaptureLoginAudit(ctx, child, id)
			if noticeErr == nil {
				noticeErr = child.Commit(ctx)
				if noticeErr != nil {
					_ = child.Rollback(ctx)
				}
			} else {
				_ = child.Rollback(ctx)
			}
		}
		if noticeErr != nil {
			slog.Error("merchant login notice source unavailable")
		}
	}
	return tx.Commit(ctx)
}
