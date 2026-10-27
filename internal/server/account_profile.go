package server

import (
	"context"
	"strconv"
	"time"
)

type merchantAccountProfile struct {
	Name                  string `json:"name"`
	Email                 string `json:"email"`
	Pay                   string `json:"pay"`
	UID                   string `json:"uid"`
	AddTime               string `json:"addtime"`
	RegistrationTimeKnown bool   `json:"registration_time_known"`
	HasPassword           bool   `json:"has_pwd"`
}

var accountDisplayZone = time.FixedZone("Asia/Shanghai", 8*3600)

func (s *Server) readMerchantProfile(ctx context.Context, merchant int64, username string) (merchantAccountProfile, error) {
	out := merchantAccountProfile{UID: strconv.FormatInt(merchant, 10)}
	var born time.Time
	e := s.Pool.QueryRow(ctx, `SELECT name,COALESCE((SELECT email FROM enrollment_emails WHERE merchant_id=m.id),profile->>'email',''),COALESCE(to_jsonb(m)->>'pay','1'),created_at,registration_time_known,
 EXISTS(SELECT 1 FROM console_users u WHERE u.role='merchant' AND u.subject_id=m.id AND u.username=$2 AND u.enabled AND u.password_hash<>'') FROM merchants m WHERE id=$1`, merchant, username).Scan(&out.Name, &out.Email, &out.Pay, &born, &out.RegistrationTimeKnown, &out.HasPassword)
	if e != nil {
		return merchantAccountProfile{}, e
	}
	if out.RegistrationTimeKnown {
		out.AddTime = born.In(accountDisplayZone).Format("2006-01-02 15:04:05")
	}
	return out, nil
}
