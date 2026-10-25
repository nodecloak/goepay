package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// This uses the existing code HMAC so migration backfill protects historical
// payment claims. Purpose is deliberately absent from the web uniqueness key.
func (s *Store) claimOAuthAuthorizationCode(ctx context.Context, tx pgx.Tx, provider, app, domain, code string, claim []byte) (bool, error) {
	// Alipay web and mini both redeem alipay.system.oauth.token. A different
	// presentation purpose cannot grant another redemption of the same code.
	if provider == "alipay" && domain == "mini" {
		domain = "web"
	}
	hash := s.oauthHash("code", code)
	var used bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM oauth_code_claims WHERE provider=$1 AND app_id=$2 AND code_hash=$3 AND COALESCE(state_hash,mini_state_hash) IS DISTINCT FROM $4::bytea) OR EXISTS(SELECT 1 FROM oauth_authorization_code_claims WHERE provider=$1 AND app_id=$2 AND code_hash=$3 AND ($1='alipay' OR domain=$5))`, provider, app, hash, claim, domain).Scan(&used); e != nil {
		return false, e
	}
	if used {
		return false, nil
	}
	tag, e := tx.Exec(ctx, `INSERT INTO oauth_authorization_code_claims(provider,app_id,domain,code_hash,claim_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, provider, app, domain, s.oauthHash("code", code), claim)
	return tag.RowsAffected() == 1, e
}
