package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/privatesearch"
	"strconv"
	"strings"
	"unicode/utf8"
)

type SettlementRecipientActor struct {
	Role, Class, Name string
	ID                int64
}

func (a SettlementRecipientActor) valid(mid int64) bool {
	return a.ID > 0 && a.Name != "" && len(a.Name) <= 128 && !strings.ContainsAny(a.Name, "\r\n\x00") && (a.Role == "merchant" && a.ID == mid || a.Role == "admin" && (a.Class == "super" || a.Class == "operator"))
}

type SettlementRecipientInput struct {
	Type     string `json:"type"`
	Account  string `json:"account"`
	Name     string `json:"name"`
	BankCode string `json:"bank_code"`
	AppID    string `json:"app_id"`
}
type SettlementRecipientView struct {
	State         string `json:"state"`
	ConfigID      int64  `json:"config_id,string"`
	Type          string `json:"type"`
	AppID         string `json:"app_id"`
	AccountMasked string `json:"account_masked"`
	NameMasked    string `json:"name_masked"`
}

func normalizeSettlementRecipient(in SettlementRecipientInput) (SettlementRecipientInput, error) {
	in.Account = strings.TrimSpace(in.Account)
	in.Name = strings.TrimSpace(in.Name)
	for _, v := range []string{in.Type, in.Account, in.Name, in.BankCode, in.AppID} {
		if !utf8.ValidString(v) || strings.ContainsAny(v, "\x00\r\n") {
			return in, ErrInvalid
		}
	}
	if in.Account == "" || in.Name == "" || len(in.Account) > 128 || len(in.Name) > 128 || len(in.AppID) > 128 || len(in.BankCode) > 32 {
		return in, ErrInvalid
	}
	switch in.Type {
	case "alipay":
		if in.BankCode != "" {
			return in, ErrInvalid
		}
	case "wxpay":
		if in.AppID == "" || in.BankCode != "" {
			return in, ErrInvalid
		}
	case "bank":
		if len(in.Account) < 8 || len(in.Account) > 32 || in.AppID != "" {
			return in, ErrInvalid
		}
		for _, c := range in.Account {
			if c < '0' || c > '9' {
				return in, ErrInvalid
			}
		}
		for _, c := range in.BankCode {
			if c < '0' || c > '9' {
				return in, ErrInvalid
			}
		}
	default:
		return in, ErrInvalid
	}
	return in, nil
}
func (s *Store) settlementRecipientHash(in SettlementRecipientInput) ([]byte, error) {
	if len(s.indexKey) != 32 {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	mac := hmac.New(sha256.New, s.indexKey)
	mac.Write([]byte("goepay/settlement/recipient/v1\x00"))
	mac.Write(raw)
	return mac.Sum(nil), nil
}
func (s *Store) encryptSettlementRecipient(purpose string, id int64, in SettlementRecipientInput) ([]byte, error) {
	if s.aead == nil || id <= 0 {
		return nil, ErrInvalid
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return s.aead.Seal(nonce, nonce, raw, []byte(fmt.Sprintf("goepay/settlement/%s/v1/%d", purpose, id))), nil
}
func (s *Store) decryptSettlementRecipient(purpose string, id int64, cipher []byte) (SettlementRecipientInput, error) {
	var in SettlementRecipientInput
	if s.aead == nil || id <= 0 || len(cipher) < s.aead.NonceSize() {
		return in, ErrInvalid
	}
	n := s.aead.NonceSize()
	raw, e := s.aead.Open(nil, cipher[:n], cipher[n:], []byte(fmt.Sprintf("goepay/settlement/%s/v1/%d", purpose, id)))
	if e != nil {
		return in, e
	}
	if e = json.Unmarshal(raw, &in); e != nil {
		return in, e
	}
	return normalizeSettlementRecipient(in)
}
func recipientView(state string, id int64, in SettlementRecipientInput) SettlementRecipientView {
	masked := "***"
	r := []rune(in.Account)
	if len(r) >= 8 {
		masked += string(r[len(r)-4:])
	}
	return SettlementRecipientView{State: state, ConfigID: id, Type: in.Type, AppID: in.AppID, AccountMasked: masked, NameMasked: "***"}
}

type recipientRecord struct {
	ID, MerchantID int64
	Type, AppID    string
	Hash, Cipher   []byte
}

func (s *Store) readSettlementConfig(ctx context.Context, q fundsReader, mid int64) (recipientRecord, SettlementRecipientInput, error) {
	var c recipientRecord
	var in SettlementRecipientInput
	e := q.QueryRow(ctx, "SELECT c.id,c.merchant_id,c.recipient_type,c.app_id,c.identity_hash,c.recipient_cipher FROM settlement_recipient_current p JOIN settlement_recipient_configs c ON c.id=p.config_id AND c.merchant_id=p.merchant_id WHERE p.merchant_id=$1", mid).Scan(&c.ID, &c.MerchantID, &c.Type, &c.AppID, &c.Hash, &c.Cipher)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, in, ErrNotFound
	}
	if e != nil {
		return c, in, e
	}
	in, e = s.decodeSettlementConfig(c, mid)
	return c, in, e
}

func (s *Store) decodeSettlementConfig(c recipientRecord, mid int64) (SettlementRecipientInput, error) {
	var in SettlementRecipientInput
	if c.ID <= 0 || mid <= 0 || c.MerchantID != mid {
		return in, ErrConflict
	}
	in, e := s.decryptSettlementRecipient("config", c.ID, c.Cipher)
	if e != nil {
		return in, e
	}
	digest, e := s.settlementRecipientHash(in)
	if e != nil {
		return in, e
	}
	if !hmac.Equal(digest, c.Hash) || in.Type != c.Type || in.AppID != c.AppID {
		return in, ErrConflict
	}
	return in, nil
}
func (s *Store) SettlementRecipient(ctx context.Context, mid int64) (SettlementRecipientView, error) {
	if mid <= 0 {
		return SettlementRecipientView{}, ErrInvalid
	}
	c, in, e := s.readSettlementConfig(ctx, s.pool, mid)
	if errors.Is(e, ErrNotFound) {
		return SettlementRecipientView{State: "missing"}, nil
	}
	if e != nil {
		return SettlementRecipientView{}, e
	}
	return recipientView("configured", c.ID, in), nil
}
func (s *Store) SetSettlementRecipient(ctx context.Context, mid int64, input SettlementRecipientInput, expectedID *int64, actor SettlementRecipientActor) (SettlementRecipientView, error) {
	if !actor.valid(mid) || expectedID != nil && *expectedID < 0 {
		return SettlementRecipientView{}, ErrInvalid
	}
	in, e := normalizeSettlementRecipient(input)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	digest, e := s.settlementRecipientHash(in)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&enabled); e != nil {
		return SettlementRecipientView{}, e
	}
	if !enabled {
		return SettlementRecipientView{}, ErrState
	}
	old, _, e := s.readSettlementConfig(ctx, tx, mid)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return SettlementRecipientView{}, e
	}
	if expectedID != nil && *expectedID != old.ID {
		return SettlementRecipientView{}, ErrConflict
	}
	if old.ID > 0 && hmac.Equal(old.Hash, digest) {
		return recipientView("configured", old.ID, in), tx.Commit(ctx)
	}
	gate, e := requireSettlementType(ctx, tx, mid, in.Type, false)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	view, e := s.recordSettlementRecipientTx(ctx, tx, mid, in, digest, actor)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	if e = gate.recheck(ctx, tx); e != nil {
		return SettlementRecipientView{}, e
	}
	return view, tx.Commit(ctx)
}

// SetInitialSettlementRecipientTx records the first settlement recipient of a
// merchant an administrator is creating (添加商户 结算信息), inside the
// caller's creation transaction. It never replaces a recipient: a merchant that
// already has one is a conflict, and changes go through SetSettlementRecipient.
func (s *Store) SetInitialSettlementRecipientTx(ctx context.Context, tx pgx.Tx, mid int64, input SettlementRecipientInput, actor SettlementRecipientActor) (SettlementRecipientView, error) {
	if tx == nil || actor.Role != "admin" || !actor.valid(mid) {
		return SettlementRecipientView{}, ErrInvalid
	}
	in, e := normalizeSettlementRecipient(input)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	digest, e := s.settlementRecipientHash(in)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	var found bool
	if e = tx.QueryRow(ctx, "SELECT true FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&found); e != nil {
		return SettlementRecipientView{}, e
	}
	if _, _, e = s.readSettlementConfig(ctx, tx, mid); e == nil {
		return SettlementRecipientView{}, ErrConflict
	} else if !errors.Is(e, ErrNotFound) {
		return SettlementRecipientView{}, e
	}
	gate, e := requireSettlementType(ctx, tx, mid, in.Type, true)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	view, e := s.recordSettlementRecipientTx(ctx, tx, mid, in, digest, actor)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	if e = gate.recheck(ctx, tx); e != nil {
		return SettlementRecipientView{}, e
	}
	return view, nil
}

// recordSettlementRecipientTx stores a normalized recipient as a new encrypted
// version and makes it current, with its private search index and audit row.
func (s *Store) recordSettlementRecipientTx(ctx context.Context, tx pgx.Tx, mid int64, in SettlementRecipientInput, digest []byte, actor SettlementRecipientActor) (SettlementRecipientView, error) {
	var id int64
	if e := tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_recipient_configs','id'))").Scan(&id); e != nil {
		return SettlementRecipientView{}, e
	}
	cipher, e := s.encryptSettlementRecipient("config", id, in)
	if e != nil {
		return SettlementRecipientView{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_recipient_configs(id,merchant_id,recipient_type,app_id,identity_hash,recipient_cipher) VALUES($1,$2,$3,$4,$5,$6)", id, mid, in.Type, in.AppID, digest, cipher); e != nil {
		return SettlementRecipientView{}, e
	}
	if e = s.indexPrivateSearchTx(ctx, tx, "settlement_config", id, mid, cipher, privatesearch.Fields{privatesearch.Account: in.Account, privatesearch.Name: in.Name}); e != nil {
		return SettlementRecipientView{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_recipient_current(merchant_id,config_id) VALUES($1,$2) ON CONFLICT(merchant_id) DO UPDATE SET config_id=EXCLUDED.config_id", mid, id); e != nil {
		return SettlementRecipientView{}, e
	}
	raw, _ := json.Marshal(map[string]string{"config_id": strconv.FormatInt(id, 10), "type": in.Type})
	if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'settlement-recipient',$4,'configure',$5)", actor.Role, actor.ID, actor.Name, mid, raw); e != nil {
		return SettlementRecipientView{}, e
	}
	return recipientView("configured", id, in), nil
}
func (s *Store) snapshotSettlementRecipient(ctx context.Context, tx pgx.Tx, v LedgerRequest, expectedID *int64) error {
	c, in, e := s.readSettlementConfig(ctx, tx, v.MerchantID)
	if errors.Is(e, ErrNotFound) {
		if expectedID != nil {
			return ErrConflict
		}
		return nil
	}
	if e != nil {
		return e
	}
	if expectedID != nil && *expectedID != c.ID {
		return ErrConflict
	}
	cipher, e := s.encryptSettlementRecipient("request", v.ID, in)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO settlement_recipients(request_id,config_id,identity_hash,recipient_cipher) VALUES($1,$2,$3,$4)", v.ID, c.ID, c.Hash, cipher)
	return e
}
func (s *Store) attachSettlementRecipient(ctx context.Context, q fundsReader, v LedgerRequest) (LedgerRequest, error) {
	if v.Kind != "settlements" {
		return v, nil
	}
	view := SettlementRecipientView{State: "missing"}
	var hash, cipher []byte
	e := q.QueryRow(ctx, "SELECT config_id,identity_hash,recipient_cipher FROM settlement_recipients WHERE request_id=$1", v.ID).Scan(&view.ConfigID, &hash, &cipher)
	if errors.Is(e, pgx.ErrNoRows) {
		v.SettlementRecipient = &view
		return v, nil
	}
	if e != nil {
		return v, e
	}
	in, e := s.decryptSettlementRecipient("request", v.ID, cipher)
	if e != nil {
		return v, e
	}
	digest, e := s.settlementRecipientHash(in)
	if e != nil {
		return v, e
	}
	if !hmac.Equal(digest, hash) {
		return v, ErrConflict
	}
	view = recipientView("recorded", view.ConfigID, in)
	v.SettlementRecipient = &view
	return v, nil
}
func (s *Store) SettlementRequest(ctx context.Context, mid, id int64) (LedgerRequest, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return LedgerRequest{}, e
	}
	defer tx.Rollback(ctx)
	v, e := scanLedgerRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM ledger_requests WHERE id=$1 AND merchant_id=$2 AND kind='settlements'", id, mid))
	if e != nil {
		return v, e
	}
	v, e = attachSettlementTerms(ctx, tx, v)
	if e != nil {
		return v, e
	}
	v, e = s.attachSettlementRecipient(ctx, tx, v)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
