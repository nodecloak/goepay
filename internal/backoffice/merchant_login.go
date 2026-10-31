package backoffice

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

func merchantProfile(fields map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range fields {
		derived := false
		for _, f := range definitions["merchants"].fields {
			if f.Name == k && f.ReadOnly {
				derived = true
				break
			}
		}
		if derived || merchantCreateSetupFields[k] {
			continue
		}
		switch k {
		case "pay", "id", "name", "enabled", "signature_algorithm", "api_key", "rsa_public_key", "login_username", "login_password", "auth_version", "base_group_id", "group_baseline_unresolved", "confirm_group_assignment", "group_assignment":
			continue
		}
		out[k] = v
	}
	return out
}
func syncMerchantLogin(ctx context.Context, tx pgx.Tx, id int64, fields map[string]any, revoke bool) error {
	username, _ := fields["login_username"].(string)
	password, _ := fields["login_password"].(string)
	if username == "" && password != "" {
		return ErrInvalid
	}
	if username != "" && !usernamePattern.MatchString(username) {
		return ErrInvalid
	}
	if revoke {
		if _, err := tx.Exec(ctx, "UPDATE console_users SET auth_version=auth_version+1 WHERE role='merchant' AND subject_id=$1", id); err != nil {
			return err
		}
	}
	if username == "" {
		return nil
	}
	var current string
	err := tx.QueryRow(ctx, "SELECT username FROM console_users WHERE role='merchant' AND subject_id=$1 ORDER BY id LIMIT 1 FOR UPDATE", id).Scan(&current)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && current != username {
		return ErrForbidden
	}
	if password == "" {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("%w: initial login password required", ErrInvalid)
		}
		return nil
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return ErrInvalid
	}
	if current == "" {
		_, err = tx.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES($1,'merchant',$2,$3,$4)", username, id, fmt.Sprint(fields["name"]), hash)
	} else {
		_, err = tx.Exec(ctx, "UPDATE console_users SET password_hash=$3 WHERE username=$1 AND role='merchant' AND subject_id=$2", username, id, hash)
	}
	return businessError(err)
}
