package enrollment

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// Original user_review uses 0/1 strings, independently from registration_open.
// Do not interpret JSON booleans, nulls, duplicates or arbitrary integers.
func registrationPayStatus(ctx context.Context, tx pgx.Tx) (int, error) {
	rows, err := tx.Query(ctx, `SELECT body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='user_review' ORDER BY id FOR SHARE`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	status, count := 1, 0
	for rows.Next() {
		count++
		var raw []byte
		var value string
		if err = rows.Scan(&raw); err != nil {
			return 0, err
		}
		if count > 1 || len(raw) < 2 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil || value != "0" && value != "1" {
			return 0, ErrInvalid
		}
		if value == "1" {
			status = 2
		}
	}
	return status, rows.Err()
}
