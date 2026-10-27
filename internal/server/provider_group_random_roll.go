package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"math/big"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/payment"
)

// Choose an enabled global group before applying its original limits/strategy.
// Failure of the chosen group never causes a second draw or a provider request.
func readRandomGroupRoll(ctx context.Context, tx pgx.Tx, kind string) (int64, []byte, backoffice.PayRoll, error) {
	type candidate struct {
		id   int64
		body []byte
		rule backoffice.PayRoll
	}
	rows, e := tx.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='pay-rolls' AND body->>'merchant_id'='0' AND body->>'type'=$1 AND body->'enabled'='true'::jsonb ORDER BY id LIMIT 1001 FOR SHARE`, kind)
	if e != nil {
		return 0, nil, backoffice.PayRoll{}, e
	}
	defer rows.Close()
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.id, &c.body); e != nil {
			return 0, nil, backoffice.PayRoll{}, e
		}
		if len(candidates) == 1000 {
			return 0, nil, backoffice.PayRoll{}, payment.ErrInvalid
		}
		var fields map[string]any
		if json.Unmarshal(c.body, &fields) != nil {
			return 0, nil, backoffice.PayRoll{}, payment.ErrInvalid
		}
		c.rule, e = backoffice.ParsePayRoll(fields)
		if e != nil {
			return 0, nil, backoffice.PayRoll{}, e
		}
		if !c.rule.Enabled || c.rule.MerchantID != 0 || c.rule.Type != kind {
			return 0, nil, backoffice.PayRoll{}, payment.ErrInvalid
		}
		candidates = append(candidates, c)
	}
	if e = rows.Err(); e != nil {
		return 0, nil, backoffice.PayRoll{}, e
	}
	if len(candidates) == 0 {
		return 0, nil, backoffice.PayRoll{}, payment.ErrState
	}
	n, e := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	if e != nil {
		return 0, nil, backoffice.PayRoll{}, e
	}
	c := candidates[n.Int64()]
	return c.id, c.body, c.rule, nil
}
