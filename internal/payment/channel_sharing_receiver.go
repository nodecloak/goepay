package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type channelSharingBinding struct {
	MerchantID, ChannelID                  int64
	Provider, AppID, Product, AccountScope string
	RelationScope                          string
}
type ChannelSharingReceiverView struct {
	Operation     *ChannelSharingReceiverOperation `json:"operation,omitempty"`
	ID            int64                            `json:"id,string"`
	MerchantID    int64                            `json:"merchant_id,string"`
	ChannelID     int64                            `json:"channel_id,string"`
	Provider      string                           `json:"provider"`
	AppID         string                           `json:"app_id"`
	Product       string                           `json:"product"`
	Type          string                           `json:"type"`
	AccountMasked string                           `json:"account_masked"`
	NameMasked    string                           `json:"name_masked"`
	State         string                           `json:"state"`
	CreatedAt     time.Time                        `json:"created_at"`
}
type sharingReceiverRecord struct {
	ID                                                                                  int64
	Binding                                                                             channelSharingBinding
	ExternalNo, State                                                                   string
	AccountHash, InputHash, RelationAccountHash, RelationHash, RelationAuthHash, Cipher []byte
	CreatedAt                                                                           time.Time
	LastOperation                                                                       int64
}

var channelSharingExternalNo = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var sharingCanonicalAlipayUID = regexp.MustCompile(`^2088[0-9]{12}$`)

func sharingSafeField(value string, max int, required bool) bool {
	if required && value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func validSharingBinding(b channelSharingBinding) bool {
	return validSharingPublicBinding(b) && sharingSafeField(b.AccountScope, 4096, true) && sharingSafeField(b.RelationScope, 4096, true)
}
func validSharingPublicBinding(b channelSharingBinding) bool {
	return b.MerchantID > 0 && b.ChannelID > 0 && sharingSafeField(b.AppID, 128, true) && (b.Provider == "wxpay" && wechatSharingProduct(b.Product) || b.Provider == "alipay" && b.Product == "alipay_direct")
}
func wechatSharingProduct(product string) bool {
	return product == "wxpay_v3_direct" || product == "wxpay_v2_single_direct"
}
func validSharingReceiverInput(b channelSharingBinding, r adapter.SharingReceiver) bool {
	accountLimit, nameLimit := 128, 256
	if b.Product == "wxpay_v2_single_direct" {
		accountLimit, nameLimit = 64, 1024
	}
	if !sharingSafeField(r.Account, accountLimit, true) || !sharingSafeField(r.Name, nameLimit, false) {
		return false
	}
	if b.Provider == "alipay" {
		return (r.Type == "userId" || r.Type == "loginName" || r.Type == "openId") && r.RelationType == "" && r.CustomRelation == ""
	}
	if r.Type != "MERCHANT_ID" && r.Type != "PERSONAL_OPENID" || r.Type == "MERCHANT_ID" && r.Name == "" {
		return false
	}
	switch r.RelationType {
	case "SERVICE_PROVIDER", "STORE", "STAFF", "STORE_OWNER", "PARTNER", "HEADQUARTER", "BRAND", "DISTRIBUTOR", "USER", "SUPPLIER":
		return r.CustomRelation == ""
	case "CUSTOM":
		if b.Product == "wxpay_v2_single_direct" {
			return sharingSafeField(r.CustomRelation, 40, true) && utf8.RuneCountInString(r.CustomRelation) <= 10
		}
		return sharingSafeField(r.CustomRelation, 30, true)
	}
	return false
}
func (s *Store) sharingDigest(domain string, raw []byte) ([]byte, error) {
	if len(s.indexKey) != 32 || domain == "" || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, s.indexKey)
	mac.Write([]byte("goepay/channel-sharing/" + domain + "/v1\x00"))
	mac.Write(raw)
	return mac.Sum(nil), nil
}
func (s *Store) sharingReceiverRelationHash(b channelSharingBinding, physicalAccount []byte, r adapter.SharingReceiver) ([]byte, error) {
	account := r.Account
	if b.Provider == "alipay" && r.Type == "loginName" {
		account = strings.ToLower(account)
	}
	app := ""
	if b.Provider == "wxpay" && r.Type == "PERSONAL_OPENID" {
		app = b.AppID
	}
	raw, e := json.Marshal([]any{b.Provider, app, hex.EncodeToString(physicalAccount), r.Type, account})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("physical-receiver-relation", raw)
}
func (s *Store) sharingReceiverAuthHash(b channelSharingBinding, r adapter.SharingReceiver, physicalHash []byte) ([]byte, error) {
	if b.Provider != "alipay" {
		return physicalHash, nil
	}
	raw, e := json.Marshal([]string{b.Provider, b.AppID, r.Type, r.Account})
	if e != nil {
		return nil, e
	}
	return s.sharingDigest("authenticated-receiver-relation", raw)
}
func sharingReceiverAAD(id int64, b channelSharingBinding, accountHash, physicalAccount []byte) []byte {
	raw, _ := json.Marshal([]any{"goepay/channel-sharing/receiver/v1", id, b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Product, hex.EncodeToString(accountHash), hex.EncodeToString(physicalAccount)})
	return raw
}
func (s *Store) sealChannelSharingReceiver(id int64, b channelSharingBinding, r adapter.SharingReceiver) ([]byte, error) {
	if s.aead == nil || id <= 0 {
		return nil, ErrInvalid
	}
	account, e := s.sharingDigest("account", []byte(b.AccountScope))
	if e != nil {
		return nil, e
	}
	physical, e := s.sharingDigest("physical-relation-account", []byte(b.RelationScope))
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return s.aead.Seal(nonce, nonce, raw, sharingReceiverAAD(id, b, account, physical)), nil
}
func (s *Store) openSharingReceiverRecord(record sharingReceiverRecord) (adapter.SharingReceiver, error) {
	var result adapter.SharingReceiver
	if s.aead == nil || len(record.Cipher) < s.aead.NonceSize()+s.aead.Overhead() {
		return result, ErrInvalid
	}
	n := s.aead.NonceSize()
	raw, e := s.aead.Open(nil, record.Cipher[:n], record.Cipher[n:], sharingReceiverAAD(record.ID, record.Binding, record.AccountHash, record.RelationAccountHash))
	if e != nil {
		return result, ErrInvalid
	}
	if json.Unmarshal(raw, &result) != nil || !validSharingReceiverInput(record.Binding, result) {
		return result, ErrInvalid
	}
	if len(record.RelationHash) > 0 {
		hash, e := s.sharingReceiverRelationHash(record.Binding, record.RelationAccountHash, result)
		if e != nil || !hmac.Equal(hash, record.RelationHash) {
			return result, ErrConflict
		}
		if len(record.RelationAuthHash) > 0 {
			auth, e := s.sharingReceiverAuthHash(record.Binding, result, hash)
			if e != nil || !hmac.Equal(auth, record.RelationAuthHash) {
				return result, ErrConflict
			}
		}
	}
	return result, nil
}
func (s *Store) openChannelSharingReceiver(id int64, b channelSharingBinding, cipher []byte) (adapter.SharingReceiver, error) {
	account, e := s.sharingDigest("account", []byte(b.AccountScope))
	if e != nil {
		return adapter.SharingReceiver{}, e
	}
	physical, e := s.sharingDigest("physical-relation-account", []byte(b.RelationScope))
	if e != nil {
		return adapter.SharingReceiver{}, e
	}
	return s.openSharingReceiverRecord(sharingReceiverRecord{ID: id, Binding: b, AccountHash: account, RelationAccountHash: physical, Cipher: cipher})
}

const sharingReceiverColumns = "id,merchant_id,channel_id,external_no,provider,app_id,product,account_hash,input_hash,relation_account_hash,relation_hash,relation_auth_hash,receiver_cipher,state,created_at,COALESCE(last_operation_id,0)"

func scanSharingReceiver(row pgx.Row) (sharingReceiverRecord, error) {
	var c sharingReceiverRecord
	e := row.Scan(&c.ID, &c.Binding.MerchantID, &c.Binding.ChannelID, &c.ExternalNo, &c.Binding.Provider, &c.Binding.AppID, &c.Binding.Product, &c.AccountHash, &c.InputHash, &c.RelationAccountHash, &c.RelationHash, &c.RelationAuthHash, &c.Cipher, &c.State, &c.CreatedAt, &c.LastOperation)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return c, e
}
func sharingReceiverView(record sharingReceiverRecord, r adapter.SharingReceiver) ChannelSharingReceiverView {
	masked := "***"
	letters := []rune(r.Account)
	if len(letters) >= 8 {
		masked += string(letters[len(letters)-4:])
	}
	name := ""
	if r.Name != "" {
		name = "***"
	}
	return ChannelSharingReceiverView{ID: record.ID, MerchantID: record.Binding.MerchantID, ChannelID: record.Binding.ChannelID, Provider: record.Binding.Provider, AppID: record.Binding.AppID, Product: record.Binding.Product, Type: r.Type, AccountMasked: masked, NameMasked: name, State: record.State, CreatedAt: record.CreatedAt}
}
func (s *Store) ChannelSharingReceiver(ctx context.Context, mid, id int64, actor TransferBatchActor) (ChannelSharingReceiverView, error) {
	if !actor.valid(mid) || id <= 0 {
		return ChannelSharingReceiverView{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	defer tx.Rollback(ctx)
	record, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2", mid, id))
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	input, e := s.openSharingReceiverRecord(record)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	view := sharingReceiverView(record, input)
	if record.LastOperation > 0 {
		operation, e := scanSharingReceiverOperation(tx.QueryRow(ctx, "SELECT "+sharingReceiverOperationColumns+" FROM channel_sharing_receiver_operations WHERE id=$1 AND receiver_id=$2", record.LastOperation, record.ID))
		if e != nil {
			return ChannelSharingReceiverView{}, e
		}
		view.Operation = &operation
	}
	return view, tx.Commit(ctx)
}
func sharingConfigurationAudit(ctx context.Context, tx pgx.Tx, actor TransferBatchActor, resource string, id int64, action, state string) error {
	raw, _ := json.Marshal(map[string]string{"id": fmt.Sprint(id), "state": state})
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,$5,$6,$7)`, actor.Role, actor.ID, actor.Name, resource, id, action, raw)
	return e
}
func (s *Store) createChannelSharingReceiver(ctx context.Context, b channelSharingBinding, no string, input adapter.SharingReceiver, actor TransferBatchActor) (ChannelSharingReceiverView, error) {
	if !actor.valid(b.MerchantID) || !validSharingBinding(b) || !channelSharingExternalNo.MatchString(no) || !validSharingReceiverInput(b, input) || s.aead == nil {
		return ChannelSharingReceiverView{}, ErrInvalid
	}
	// A login name or OpenID may alias a userId on the same Alipay account.
	// Until a signed canonical mapping is stored, persistence accepts userId
	// only; accepting an unresolved alias would make unbind protections unsafe.
	if b.Provider == "alipay" && input.Type != "userId" {
		return ChannelSharingReceiverView{}, adapter.ErrUnsupported
	}
	if b.Provider == "alipay" && !sharingCanonicalAlipayUID.MatchString(input.Account) {
		return ChannelSharingReceiverView{}, ErrInvalid
	}
	account, e := s.sharingDigest("account", []byte(b.AccountScope))
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	physical, e := s.sharingDigest("physical-relation-account", []byte(b.RelationScope))
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	raw, e := json.Marshal([]any{b.MerchantID, b.ChannelID, b.Provider, b.AppID, b.Product, hex.EncodeToString(account), hex.EncodeToString(physical), input})
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	fingerprint, e := s.sharingDigest("receiver-input", raw)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR SHARE", b.MerchantID).Scan(&enabled); e != nil || !enabled {
		return ChannelSharingReceiverView{}, ErrState
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,48))", fmt.Sprintf("channel-sharing-receiver:%d:%s", b.MerchantID, no)); e != nil {
		return ChannelSharingReceiverView{}, e
	}
	old, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND external_no=$2", b.MerchantID, no))
	if e == nil {
		if !hmac.Equal(old.InputHash, fingerprint) {
			return ChannelSharingReceiverView{}, ErrConflict
		}
		original, e := s.openSharingReceiverRecord(old)
		if e != nil {
			return ChannelSharingReceiverView{}, e
		}
		return sharingReceiverView(old, original), nil
	}
	if !errors.Is(e, ErrNotFound) {
		return ChannelSharingReceiverView{}, e
	}
	relation, e := s.sharingReceiverRelationHash(b, physical, input)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	auth, e := s.sharingReceiverAuthHash(b, input, relation)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	// A draft is an owner-local encrypted snapshot. Only Bind may claim the
	// physical/authenticated relationship; drafts cannot reserve another owner's
	// beneficiary or pretend that a provider has verified this name.
	var id int64
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('channel_sharing_receivers','id'))").Scan(&id); e != nil {
		return ChannelSharingReceiverView{}, e
	}
	encrypted, e := s.sealChannelSharingReceiver(id, b, input)
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	record, e := scanSharingReceiver(tx.QueryRow(ctx, "INSERT INTO channel_sharing_receivers(id,merchant_id,channel_id,external_no,provider,app_id,product,account_hash,input_hash,relation_account_hash,relation_hash,relation_auth_hash,receiver_cipher) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING "+sharingReceiverColumns, id, b.MerchantID, b.ChannelID, no, b.Provider, b.AppID, b.Product, account, fingerprint, physical, relation, auth, encrypted))
	if e != nil {
		return ChannelSharingReceiverView{}, e
	}
	if e = sharingConfigurationAudit(ctx, tx, actor, "channel-sharing-receivers", id, "create", "draft"); e != nil {
		return ChannelSharingReceiverView{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ChannelSharingReceiverView{}, e
	}
	return sharingReceiverView(record, input), nil
}
