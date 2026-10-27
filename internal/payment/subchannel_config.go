package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/grouppolicy"
	"goepay/internal/identity"
	"sort"
	"strings"
	"time"
)

type SubchannelActor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}

func (a SubchannelActor) check(ctx context.Context, tx pgx.Tx) error {
	if a.Delegated {
		return identity.ErrOriginalMerchantSession
	}
	if a.User.Role == "admin" {
		return identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, false)
	}
	if err := a.lockMerchant(ctx, tx); err != nil {
		return err
	}
	return identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF)
}

// Merchant edits lock merchants before invalidating console users. Read
// projections retain session row locks and must use that same order.
func (a SubchannelActor) lockMerchant(ctx context.Context, tx pgx.Tx) error {
	if a.User.Role != "merchant" {
		return nil
	}
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM merchants WHERE id=$1 FOR SHARE`, a.User.ID).Scan(&id); err != nil {
		var pgerr *pgconn.PgError
		// A repeatable-read projection may have started before the revoke
		// committed. Its stale session must be rejected, never retried as a write.
		if errors.As(err, &pgerr) && pgerr.Code == "40001" {
			return identity.ErrOriginalMerchantSession
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrOriginalMerchantSession
		}
		return err
	}
	return nil
}
func (a SubchannelActor) allows(mid int64) bool {
	return mid > 0 && !a.Delegated && (a.User.Role == "admin" && a.User.AdminClass == "super" || a.User.Role == "merchant" && a.User.ID == mid)
}

type SubchannelStatistics struct {
	Attempts        int64  `json:"attempts,string"`
	Confirmed       int64  `json:"confirmed,string"`
	TodayAmount     int64  `json:"today_amount,string"`
	YesterdayAmount int64  `json:"yesterday_amount,string"`
	SuccessBPS      *int64 `json:"success_bps"`
	Timezone        string `json:"timezone"`
	Basis           string `json:"basis"`
}
type PaymentSubchannel struct {
	CoreRisk             *CoreRiskStatus        `json:"core_risk,omitempty"`
	ParentCoreRisk       *CoreRiskStatus        `json:"parent_core_risk,omitempty"`
	RollType             int                    `json:"roll_type"`
	FeeRateBPS           *int64                 `json:"fee_rate_bps"`
	FeeSource            string                 `json:"fee_source"`
	FeePolicy            string                 `json:"fee_policy"`
	Methods              []string               `json:"methods"`
	AccountAvailable     bool                   `json:"account_available"`
	ParentAvailable      bool                   `json:"parent_available"`
	BrowserTestAvailable bool                   `json:"browser_test_available"`
	UnavailableReason    string                 `json:"unavailable_reason"`
	Statistics           *SubchannelStatistics  `json:"statistics,omitempty"`
	ID                   int64                  `json:"id,string"`
	MerchantID           int64                  `json:"merchant_id,string"`
	ParentID             int64                  `json:"parent_id,string"`
	PayType              string                 `json:"pay_type"`
	Name                 string                 `json:"name"`
	Enabled              bool                   `json:"enabled"`
	Archived             bool                   `json:"archived"`
	Weight               int64                  `json:"weight"`
	MinAmount            int64                  `json:"min_amount,string"`
	MaxAmount            int64                  `json:"max_amount,string"`
	Revision             int64                  `json:"revision,string"`
	AccountVersion       int64                  `json:"account_version,string"`
	Fingerprint          string                 `json:"fingerprint"`
	SchemaFingerprint    string                 `json:"schema_fingerprint"`
	Fields               []SubchannelFieldState `json:"fields"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
	proof                []byte
	account              subchannelAccount
	historyRevision      int64
}
type SubchannelFieldState struct {
	Name    string `json:"name"`
	Secret  bool   `json:"secret"`
	Present bool   `json:"present"`
}
type SubchannelWrite struct {
	MerchantID, ParentID         int64
	Name                         string
	Enabled                      bool
	Weight, MinAmount, MaxAmount int64
	Expected                     string
	Values                       map[string]string
}
type subchannelAccount struct {
	Child, Version, Merchant, Parent int64
	Kind, Schema                     string
	Nonce, Ciphertext, Proof         []byte
}

const childColumns = "id,merchant_id,parent_id,pay_type,name,enabled,weight,min_amount,max_amount,revision,account_version,proof_hash,created_at,updated_at,archived"

func scanSubchannel(row pgx.Row) (PaymentSubchannel, error) {
	var out PaymentSubchannel
	e := row.Scan(&out.ID, &out.MerchantID, &out.ParentID, &out.PayType, &out.Name, &out.Enabled, &out.Weight, &out.MinAmount, &out.MaxAmount, &out.Revision, &out.AccountVersion, &out.proof, &out.CreatedAt, &out.UpdatedAt, &out.Archived)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return out, e
}
func (s *Store) childProof(c PaymentSubchannel) ([]byte, error) {
	return s.referralDigest("payment-subchannel168/head", []any{c.ID, c.MerchantID, c.ParentID, c.PayType, c.Name, c.Enabled, c.Weight, c.MinAmount, c.MaxAmount, c.Revision, c.AccountVersion, c.Archived})
}
func (s *Store) accountAAD(a subchannelAccount) ([]byte, error) {
	return json.Marshal([]any{"payment-subchannel168/account", a.Child, a.Version, a.Merchant, a.Parent, a.Kind, a.Schema})
}
func (s *Store) accountProof(a subchannelAccount) ([]byte, error) {
	return s.referralDigest("payment-subchannel168/account-source", []any{a.Child, a.Version, a.Merchant, a.Parent, a.Kind, a.Schema, a.Nonce, a.Ciphertext})
}
func (s *Store) childAccount(ctx context.Context, q fundsReader, child, version int64) (subchannelAccount, error) {
	var a subchannelAccount
	e := q.QueryRow(ctx, `SELECT subchannel_id,version,merchant_id,parent_id,pay_type,schema_hash,nonce,ciphertext,proof_hash FROM payment_subchannel_accounts WHERE subchannel_id=$1 AND version=$2`, child, version).Scan(&a.Child, &a.Version, &a.Merchant, &a.Parent, &a.Kind, &a.Schema, &a.Nonce, &a.Ciphertext, &a.Proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, ErrConflict
	}
	if e != nil {
		return a, e
	}
	proof, e := s.accountProof(a)
	if e != nil || !hmac.Equal(proof, a.Proof) {
		return a, ErrConflict
	}
	return a, nil
}

type subchannelAccountMaterial struct {
	Values         map[string]string
	Snapshot       []byte
	ParentOrigin   int64  `json:",omitempty"`
	ParentSnapshot []byte `json:",omitempty"`
}

func childSealedMaterial(factory adapter.SubchannelFactory, values map[string]string, snapshot []byte) (subchannelAccountMaterial, error) {
	m := subchannelAccountMaterial{Values: values, Snapshot: snapshot}
	if source, ok := factory.(interface{ ParentSubchannelOrigin() (int64, []byte) }); ok {
		m.ParentOrigin, m.ParentSnapshot = source.ParentSubchannelOrigin()
		if (m.ParentOrigin == 0) != (len(m.ParentSnapshot) == 0) || m.ParentOrigin < 0 || len(m.ParentSnapshot) > 1048576 || m.ParentOrigin == factory.Schema().ParentID {
			return m, ErrInvalid
		}
	}
	return m, nil
}

func (s *Store) openChildAccount(a subchannelAccount) (subchannelAccountMaterial, error) {
	if s.aead == nil || len(a.Nonce) != s.aead.NonceSize() {
		return subchannelAccountMaterial{}, ErrConflict
	}
	aad, e := s.accountAAD(a)
	if e != nil {
		return subchannelAccountMaterial{}, e
	}
	raw, e := s.aead.Open(nil, a.Nonce, a.Ciphertext, aad)
	if e != nil {
		return subchannelAccountMaterial{}, ErrConflict
	}
	var values subchannelAccountMaterial
	if json.Unmarshal(raw, &values) != nil || len(values.Values) == 0 || len(values.Snapshot) == 0 {
		return subchannelAccountMaterial{}, ErrConflict
	}
	if (values.ParentOrigin == 0) != (len(values.ParentSnapshot) == 0) || values.ParentOrigin < 0 || values.ParentOrigin == a.Parent || len(values.ParentSnapshot) > 1048576 {
		return subchannelAccountMaterial{}, ErrConflict
	}
	return values, nil
}
func (s *Store) readSubchannel(ctx context.Context, q referralReader, row pgx.Row) (PaymentSubchannel, error) {
	c, e := scanSubchannel(row)
	if e != nil {
		return c, e
	}
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='payment-subchannels' AND resource_id=$1 AND operation IN ('payment-subchannel-create','payment-subchannel-update','payment-subchannel-stop','payment-subchannel-archive')`, c.ID).Scan(&c.historyRevision); e != nil {
		return c, e
	}
	a, e := s.childAccount(ctx, q, c.ID, c.AccountVersion)
	if e != nil {
		return c, e
	}
	return s.completeSubchannel(c, a)
}
func validChildWrite(in SubchannelWrite) bool {
	return in.MerchantID > 0 && in.ParentID > 0 && len(in.Name) > 0 && len(in.Name) <= 120 && strings.TrimSpace(in.Name) == in.Name && !strings.ContainsAny(in.Name, "\x00\r\n") && in.Weight >= 1 && in.Weight <= 99 && in.MinAmount >= 0 && in.MaxAmount >= 0 && (in.MaxAmount == 0 || in.MaxAmount >= in.MinAmount)
}
func (s *Store) CreateSubchannel(ctx context.Context, a SubchannelActor, b RuntimeChannel, factory adapter.SubchannelFactory, in SubchannelWrite) (PaymentSubchannel, error) {
	var out PaymentSubchannel
	if s.aead == nil || a.User.Role != "admin" || !a.allows(in.MerchantID) || !validChildWrite(in) || factory == nil || b.id != in.ParentID {
		return out, ErrInvalid
	}
	schema := factory.Schema()
	if schema.ParentID != b.id || schema.PayType != b.kind || len(schema.Fingerprint) != 64 {
		return out, ErrInvalid
	}
	execution, e := factory.Build(in.Values)
	if e != nil || execution.Payment == nil || execution.Payment.ID() != b.kind || execution.SchemaFingerprint != schema.Fingerprint {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/channel/%d", b.id)); e != nil {
		return out, e
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	if b.parent != nil {
		if e = s.checkParentExecutionCurrent(ctx, tx, b.parent, in.MerchantID); e != nil {
			return out, e
		}
	}
	group, e := s.GroupPaymentRoutingTx(ctx, tx, in.MerchantID, b.kind)
	if e != nil {
		return out, e
	}
	if group.Choice.Kind() != grouppolicy.Subchannels {
		return out, ErrState
	}
	allowed, _, e := s.RuntimeChannelAllowedTx(ctx, tx, b)
	if e != nil {
		return out, e
	}
	if !allowed {
		return out, ErrState
	}
	out = PaymentSubchannel{MerchantID: in.MerchantID, ParentID: b.id, PayType: b.kind, Name: in.Name, Enabled: in.Enabled, Weight: in.Weight, MinAmount: in.MinAmount, MaxAmount: in.MaxAmount, Revision: 1, AccountVersion: 1}
	if e = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('payment_subchannels','id'))`).Scan(&out.ID); e != nil {
		return out, e
	}
	out.proof, e = s.childProof(out)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `INSERT INTO payment_subchannels(id,merchant_id,parent_id,pay_type,name,enabled,weight,min_amount,max_amount,revision,account_version,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,1,$10) RETURNING created_at,updated_at`, out.ID, out.MerchantID, out.ParentID, out.PayType, out.Name, out.Enabled, out.Weight, out.MinAmount, out.MaxAmount, out.proof).Scan(&out.CreatedAt, &out.UpdatedAt); e != nil {
		return out, e
	}
	schemaCommitment, e := s.childSchemaCommitment(schema.Fingerprint)
	if e != nil {
		return out, e
	}
	account := subchannelAccount{Child: out.ID, Version: 1, Merchant: out.MerchantID, Parent: b.id, Kind: b.kind, Schema: schemaCommitment}
	account.Nonce = make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(account.Nonce); e != nil {
		return out, e
	}
	aad, e := s.accountAAD(account)
	if e != nil {
		return out, e
	}
	snapshot, e := factory.Snapshot(in.Values)
	if e != nil {
		return out, ErrInvalid
	}
	material, e := childSealedMaterial(factory, in.Values, snapshot)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(material)
	if e != nil {
		return out, e
	}
	account.Ciphertext = s.aead.Seal(nil, account.Nonce, raw, aad)
	account.Proof, e = s.accountProof(account)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO payment_subchannel_accounts(subchannel_id,version,merchant_id,parent_id,pay_type,schema_hash,nonce,ciphertext,proof_hash) VALUES($1,1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, out.MerchantID, b.id, b.kind, account.Schema, account.Nonce, account.Ciphertext, account.Proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'payment-subchannels',$3,'payment-subchannel-create',jsonb_build_object('merchant_id',$4::text,'parent_id',$5::text,'account_version','1'))`, a.User.ID, a.User.Username, out.ID, fmt.Sprint(out.MerchantID), fmt.Sprint(b.id)); e != nil {
		return out, e
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return out, e
	}
	if !group.ValidUntil.IsZero() && !now.Before(group.ValidUntil) {
		return out, ErrState
	}
	out, e = s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1`, out.ID))
	if e != nil {
		return out, e
	}
	if b.parent != nil {
		if e = s.checkParentExecutionCurrent(ctx, tx, b.parent, in.MerchantID); e != nil {
			return out, e
		}
	}
	return out, tx.Commit(ctx)
}

func (s *Store) SubchannelTx(ctx context.Context, tx pgx.Tx, id, mid int64) (PaymentSubchannel, error) {
	return s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 AND merchant_id=$2 FOR SHARE`, id, mid))
}

type SubchannelValueEdit struct{ Operation, Value string }
type SubchannelPatch struct {
	Expected                     string
	Name                         *string
	Enabled                      *bool
	Weight, MinAmount, MaxAmount *int64
	ParentID                     int64
	Values                       map[string]SubchannelValueEdit
}

func (s *Store) UpdateSubchannel(ctx context.Context, a SubchannelActor, id int64, b RuntimeChannel, factory adapter.SubchannelFactory, in SubchannelPatch) (PaymentSubchannel, error) {
	var out PaymentSubchannel
	if s.aead == nil || id <= 0 || len(in.Expected) != 64 || factory == nil || b.id <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	old, e := s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 AND ($2::boolean OR merchant_id=$3)`, id, a.User.Role == "admin" && a.User.AdminClass == "super", a.User.ID))
	if e != nil {
		return out, e
	}
	if !a.allows(old.MerchantID) || a.User.Role != "admin" && (in.Name != nil || in.ParentID != 0 || in.Values != nil) {
		return out, ErrInvalid
	}
	group, e := s.GroupPaymentRoutingTx(ctx, tx, old.MerchantID, old.PayType)
	if e != nil {
		return out, e
	}
	ids := []int64{old.ParentID}
	if b.id != old.ParentID {
		ids = append(ids, b.id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, parent := range ids {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/channel/%d", parent)); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/subchannel/%d", id)); e != nil {
		return out, e
	}
	current, e := s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return out, e
	}
	if current.Archived || current.ParentID != old.ParentID || !hmac.Equal([]byte(current.Fingerprint), []byte(in.Expected)) {
		return out, ErrConflict
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	out = current
	if b.parent != nil {
		if e = s.checkParentExecutionCurrent(ctx, tx, b.parent, old.MerchantID); e != nil {
			return out, e
		}
	}
	if in.Name != nil {
		out.Name = *in.Name
	}
	if in.Enabled != nil {
		out.Enabled = *in.Enabled
	}
	if in.Weight != nil {
		out.Weight = *in.Weight
	}
	if in.MinAmount != nil {
		out.MinAmount = *in.MinAmount
	}
	if in.MaxAmount != nil {
		out.MaxAmount = *in.MaxAmount
	}
	if in.ParentID != 0 {
		out.ParentID = in.ParentID
	}
	if b.id != out.ParentID || b.kind != out.PayType || factory.Schema().ParentID != out.ParentID || factory.Schema().PayType != out.PayType {
		return out, ErrInvalid
	}
	if !validChildWrite(SubchannelWrite{MerchantID: out.MerchantID, ParentID: out.ParentID, Name: out.Name, Enabled: out.Enabled, Weight: out.Weight, MinAmount: out.MinAmount, MaxAmount: out.MaxAmount}) {
		return out, ErrInvalid
	}
	if out.Enabled {
		if group.Choice.Kind() != grouppolicy.Subchannels {
			return out, ErrState
		}
		allowed, _, e := s.RuntimeChannelAllowedTx(ctx, tx, b)
		if e != nil {
			return out, e
		}
		if !allowed {
			return out, ErrState
		}
	}
	if in.Values != nil || out.ParentID != current.ParentID {
		material, e := s.openChildAccount(current.account)
		if e != nil {
			return out, e
		}
		values := map[string]string{}
		if out.ParentID == current.ParentID {
			for k, v := range material.Values {
				values[k] = v
			}
		}
		schema := factory.Schema()
		known := map[string]bool{}
		for _, field := range schema.Fields {
			known[field.Name] = true
		}
		for key, edit := range in.Values {
			if !known[key] {
				return out, ErrInvalid
			}
			switch edit.Operation {
			case "keep":
				if edit.Value != "" || values[key] == "" {
					return out, ErrInvalid
				}
			case "replace":
				if edit.Value == "" {
					return out, ErrInvalid
				}
				values[key] = edit.Value
			case "clear":
				if edit.Value != "" {
					return out, ErrInvalid
				}
				delete(values, key)
			default:
				return out, ErrInvalid
			}
		}
		snapshot, e := factory.Snapshot(values)
		if e != nil {
			return out, ErrInvalid
		}
		out.AccountVersion++
		schemaCommitment, e := s.childSchemaCommitment(schema.Fingerprint)
		if e != nil {
			return out, e
		}
		account := subchannelAccount{Child: id, Version: out.AccountVersion, Merchant: out.MerchantID, Parent: out.ParentID, Kind: out.PayType, Schema: schemaCommitment}
		account.Nonce = make([]byte, s.aead.NonceSize())
		if _, e = rand.Read(account.Nonce); e != nil {
			return out, e
		}
		aad, e := s.accountAAD(account)
		if e != nil {
			return out, e
		}
		sealed, e := childSealedMaterial(factory, values, snapshot)
		if e != nil {
			return out, e
		}
		raw, e := json.Marshal(sealed)
		if e != nil {
			return out, e
		}
		account.Ciphertext = s.aead.Seal(nil, account.Nonce, raw, aad)
		account.Proof, e = s.accountProof(account)
		if e != nil {
			return out, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO payment_subchannel_accounts(subchannel_id,version,merchant_id,parent_id,pay_type,schema_hash,nonce,ciphertext,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, account.Version, account.Merchant, account.Parent, account.Kind, account.Schema, account.Nonce, account.Ciphertext, account.Proof); e != nil {
			return out, e
		}
	}
	if out.Enabled && in.Values == nil && out.ParentID == current.ParentID {
		commitment, e := s.childSchemaCommitment(factory.Schema().Fingerprint)
		if e != nil {
			return out, e
		}
		if commitment != current.account.Schema {
			return out, ErrState
		}
		material, e := s.openChildAccount(current.account)
		if e != nil {
			return out, e
		}
		execution, e := factory.Restore(material.Snapshot)
		if e != nil || execution.Payment == nil || execution.Payment.ID() != out.PayType {
			return out, ErrState
		}
	}
	out.Revision++
	out.proof, e = s.childProof(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_subchannels SET parent_id=$2,name=$3,enabled=$4,weight=$5,min_amount=$6,max_amount=$7,revision=$8,account_version=$9,proof_hash=$10,updated_at=clock_timestamp() WHERE id=$1`, id, out.ParentID, out.Name, out.Enabled, out.Weight, out.MinAmount, out.MaxAmount, out.Revision, out.AccountVersion, out.proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'payment-subchannels',$4,'payment-subchannel-update',jsonb_build_object('revision',$5::text,'account_version',$6::text,'enabled',$7::boolean))`, a.User.Role, a.User.ID, a.User.Username, id, fmt.Sprint(out.Revision), fmt.Sprint(out.AccountVersion), out.Enabled); e != nil {
		return out, e
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	if out.Enabled && !group.ValidUntil.IsZero() {
		var now time.Time
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return out, e
		}
		if !now.Before(group.ValidUntil) {
			return out, ErrState
		}
	}
	if b.parent != nil {
		if e = s.checkParentExecutionCurrent(ctx, tx, b.parent, old.MerchantID); e != nil {
			return out, e
		}
	}
	out, e = s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1`, id))
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (s *Store) Subchannel(ctx context.Context, a SubchannelActor, id int64) (PaymentSubchannel, error) {
	var out PaymentSubchannel
	if id <= 0 {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	out, e = s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 AND ($2::boolean OR merchant_id=$3)`, id, a.User.Role == "admin" && a.User.AdminClass == "super", a.User.ID))
	if e != nil {
		return out, e
	}
	if !a.allows(out.MerchantID) {
		return out, ErrNotFound
	}
	return out, tx.Commit(ctx)
}
