// Package orderalias120 stores administrator display metadata independently of
// payment identities. A display alias is never a provider or refund identifier.
package orderalias120

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

var ErrInvalid = errors.New("invalid independent display alias")
var ErrConflict = errors.New("independent display alias source or revision conflict")
var ErrUnavailable = errors.New("independent display alias unavailable")

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}
type View struct {
	TradeNo            string `json:"trade_no"`
	MerchantID         string `json:"merchant_id"`
	Alias              string `json:"api_trade_no_alias"`
	VerifiedAPITradeNo string `json:"verified_api_trade_no"`
	Fingerprint        string `json:"fingerprint"`
	CanUpdate          bool   `json:"can_update"`
}
type Update struct {
	Alias               string `json:"api_trade_no_alias"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
}
type Store struct {
	pool     *pgxpool.Pool
	payments *payment.Store
	key      []byte
}

func NewStore(pool *pgxpool.Pool, payments *payment.Store, master []byte) (*Store, error) {
	if pool == nil || payments == nil || len(master) < 32 {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("goepay/order-api-trade-alias120/integrity/v1"))
	return &Store{pool: pool, payments: payments, key: mac.Sum(nil)}, nil
}
func (s *Store) valid() bool {
	return s != nil && s.pool != nil && s.payments != nil && len(s.key) == sha256.Size
}
func hex64(v string) bool { return len(v) == 64 && strings.Trim(v, "0123456789abcdef") == "" }
func ValidAlias(value string) bool {
	if len(value) > 8192 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validTrade(value string) bool {
	if value == "" || len(value) > 64 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (s *Store) sum(domain string, value any) []byte {
	raw, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(domain + "\x00"))
	mac.Write(raw)
	return mac.Sum(nil)
}

func (s *Store) actor(ctx context.Context, tx pgx.Tx, a Actor, write bool) error {
	if a.User.Role != "admin" || a.Delegated {
		return identity.ErrAdminSession
	}
	if err := identity.CheckOriginalAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); err != nil {
		return err
	}
	if write {
		digest := sha256.Sum256([]byte(a.Token))
		var csrf string
		if err := tx.QueryRow(ctx, `SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE`, digest[:]).Scan(&csrf); err != nil || a.CSRF == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(a.CSRF)) != 1 {
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			return identity.ErrAdminSession
		}
	}
	return nil
}

// Locks are local alias advisory -> administrator/session -> original order
// and receipt -> alias state. Display metadata does not decrypt merchant keys
// or lock merchant credentials. Every verification uses this connection;
// there is no provider I/O, second pool acquisition, or financial write.
func (s *Store) begin(ctx context.Context, a Actor, trade string, write bool) (pgx.Tx, payment.OrderAPITradeAliasSource120, error) {
	var source payment.OrderAPITradeAliasSource120
	if !s.valid() {
		return nil, source, ErrUnavailable
	}
	if !validTrade(trade) {
		return nil, source, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, source, err
	}
	fail := func(e error) (pgx.Tx, payment.OrderAPITradeAliasSource120, error) {
		_ = tx.Rollback(ctx)
		return nil, source, e
	}
	lock := "pg_advisory_xact_lock_shared"
	if write {
		lock = "pg_advisory_xact_lock"
	}
	if _, err = tx.Exec(ctx, "SELECT "+lock+"(hashtextextended($1,120))", "goepay/order-api-trade-alias120/"+trade); err != nil {
		return fail(err)
	}
	observed, err := s.payments.OrderByTradeNoTx(ctx, tx, trade)
	if err != nil {
		return fail(err)
	}
	if err = s.actor(ctx, tx, a, write); err != nil {
		return fail(err)
	}
	source, err = s.payments.OrderAPITradeAliasSourceTx120(ctx, tx, observed.ID, observed.MerchantID, trade)
	if err != nil {
		return fail(err)
	}
	return tx, source, nil
}
func (s *Store) Read(ctx context.Context, a Actor, trade string) (View, error) {
	tx, source, err := s.begin(ctx, a, trade, false)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(ctx)
	state, err := s.load(ctx, tx, source)
	if err != nil {
		return View{}, err
	}
	if err = s.actor(ctx, tx, a, false); err != nil {
		return View{}, err
	}
	view := s.view(state, source)
	if err = tx.Commit(ctx); err != nil {
		return View{}, err
	}
	return view, nil
}
func (s *Store) Update(ctx context.Context, a Actor, trade string, input Update) (View, error) {
	if !ValidAlias(input.Alias) || !hex64(input.ExpectedFingerprint) {
		return View{}, ErrInvalid
	}
	tx, source, err := s.begin(ctx, a, trade, true)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback(ctx)
	old, err := s.load(ctx, tx, source)
	if err != nil {
		return View{}, err
	}
	if !hmac.Equal([]byte(s.view(old, source).Fingerprint), []byte(input.ExpectedFingerprint)) || old.Facts.Revision == math.MaxInt64 {
		return View{}, ErrConflict
	}
	f := aliasFacts{Version: 1, Revision: old.Facts.Revision + 1, Source: source, Alias: input.Alias, PreviousCommitment: hex.EncodeToString(old.Proof)}
	c := aliasCommit{OrderID: source.OrderID, MerchantID: source.MerchantID, Revision: f.Revision, PreviousCommitment: f.PreviousCommitment, ActorID: a.User.ID, ActorUsername: a.User.Username}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&f.SavedAt, &c.BirthTransaction); err != nil {
		return View{}, err
	}
	f.SavedAt = f.SavedAt.UTC()
	proof := s.sum("state", f)
	c.Commitment = hex.EncodeToString(proof)
	changes := map[string]string{"trade_no": source.TradeNo, "merchant_id": strconv.FormatInt(source.MerchantID, 10), "revision": strconv.FormatInt(f.Revision, 10), "commitment": c.Commitment, "previous_commitment": c.PreviousCommitment}
	rawChanges, _ := json.Marshal(changes)
	if err = tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'orders',$3,'api-trade-alias-update',$4::jsonb) RETURNING id`, a.User.ID, a.User.Username, source.OrderID, string(rawChanges)).Scan(&c.AuditID); err != nil {
		return View{}, err
	}
	c.Proof = s.commitProof(c)
	if _, err = tx.Exec(ctx, `INSERT INTO order_api_trade_alias_commits120(order_id,merchant_id,revision,commitment,previous_commitment,audit_id,actor_id,actor_username,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.OrderID, c.MerchantID, c.Revision, c.Commitment, c.PreviousCommitment, c.AuditID, c.ActorID, c.ActorUsername, c.Proof); err != nil {
		return View{}, err
	}
	rawFacts, _ := json.Marshal(f)
	if old.Configured {
		tag, err := tx.Exec(ctx, `UPDATE order_api_trade_aliases120 SET revision=$1,api_trade_no_alias=$2,facts=$3::jsonb,proof_hash=$4 WHERE order_id=$5 AND merchant_id=$6 AND revision=$7`, f.Revision, f.Alias, string(rawFacts), proof, source.OrderID, source.MerchantID, old.Facts.Revision)
		if err != nil {
			return View{}, err
		}
		if tag.RowsAffected() != 1 {
			return View{}, ErrConflict
		}
	} else if _, err = tx.Exec(ctx, `INSERT INTO order_api_trade_aliases120(order_id,merchant_id,revision,api_trade_no_alias,facts,proof_hash) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, source.OrderID, source.MerchantID, f.Revision, f.Alias, string(rawFacts), proof); err != nil {
		return View{}, err
	}
	// Flush any deferred validation before the last durable actor/clock check.
	if _, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return View{}, err
	}
	if err = s.actor(ctx, tx, a, true); err != nil {
		return View{}, err
	}
	view := s.view(aliasRecord{Facts: f, Proof: proof, Configured: true}, source)
	if err = tx.Commit(ctx); err != nil {
		return View{}, err
	}
	return view, nil
}
