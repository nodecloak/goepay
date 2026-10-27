package captcha203

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type nativeClaim205 struct {
	Native, Challenge []byte
	Purpose           string
	At                time.Time
}

func (s *Store) claimSeal205(c nativeClaim205) []byte {
	c.At = c.At.UTC()
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("native-claim205\n"))
	mac.Write(raw)
	return mac.Sum(nil)
}

// Claim is called only after authoritative remote verification, in the same
// transaction as the consumer's durable admission. Its native identity never
// contains a purpose or wrapper token: a V4 lot cannot change either to replay.
// The legacy 203 ledger is checked without rewriting any of its original proof.
func (s *Store) Claim(ctx context.Context, tx pgx.Tx, c Config, p Proof, purpose string, challenge []byte) error {
	if s == nil || tx == nil || !validConfig(c) || len(challenge) != 32 || purpose != "testpay" && purpose != "login" && purpose != "payment-defend" {
		return ErrInvalid
	}
	identity, legacy := "v4\n"+c.ID+"\n"+p.LotNumber, p.CaptchaID+"\n"+p.LotNumber
	if c.Version == 0 {
		if !identifier.MatchString(p.GeetestChallenge) {
			return ErrInvalid
		}
		identity = "v3\n" + c.ID + "\n" + p.GeetestChallenge
		legacy = p.GeetestChallenge + "\n" + p.GeetestValidate + "\n" + p.GeetestSeccode
	} else if p.CaptchaID != c.ID || !identifier.MatchString(p.LotNumber) {
		return ErrInvalid
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("native-identity205\n" + identity))
	native := mac.Sum(nil)
	old := hmac.New(sha256.New, s.legacyProofKey205)
	old.Write([]byte("native-proof\n" + legacy))
	var used bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM captcha_used_proofs203 WHERE proof_hash=$1)`, old.Sum(nil)).Scan(&used); e != nil {
		return e
	}
	if used {
		return ErrInvalid
	}
	var at time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return e
	}
	claim := nativeClaim205{Native: native, Challenge: challenge, Purpose: purpose, At: at.UTC()}
	tag, e := tx.Exec(ctx, `INSERT INTO captcha_native_claims205(native_hash,purpose,challenge_hash,consumed_at,proof_hash)VALUES($1,$2,$3,$4,$5)ON CONFLICT DO NOTHING`, native, purpose, challenge, claim.At, s.claimSeal205(claim))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var previous nativeClaim205
	var proof []byte
	if e = tx.QueryRow(ctx, `SELECT native_hash,purpose,challenge_hash,consumed_at,proof_hash FROM captcha_native_claims205 WHERE native_hash=$1`, native).Scan(&previous.Native, &previous.Purpose, &previous.Challenge, &previous.At, &proof); e != nil {
		return e
	}
	if !hmac.Equal(proof, s.claimSeal205(previous)) {
		return ErrConflict
	}
	return ErrInvalid
}
