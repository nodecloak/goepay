package businessnotice

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/businesssms"
	"goepay/internal/noticeconfig"
)

type RequestFacts struct {
	Transaction                             string
	ID, MerchantID, RecipientID, Gross, Net int64
	Kind, DisplayType                       string
	OriginalHash                            []byte
}
type RequestFactsReader interface {
	BusinessNoticeRequestFacts(context.Context, pgx.Tx, int64) (RequestFacts, error)
}
type Store struct {
	accounts231 *noticeconfig.Store
	sms231      *businesssms.Store
	pool        *pgxpool.Pool
	aead        cipher.AEAD
	macKey      []byte
	requests    RequestFactsReader
}

// Pure construction; no configuration, credentials, database or network reads.
func NewStore(pool *pgxpool.Pool, key []byte, reader RequestFactsReader) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	derive := func(purpose string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("goepay/admin-business83/key/v1/" + purpose))
		return m.Sum(nil)
	}
	block, err := aes.NewCipher(derive("encryption"))
	if err != nil {
		return nil, ErrInvalid
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalid
	}
	accounts, e := noticeconfig.FromMaster(pool, key)
	if e != nil {
		return nil, e
	}
	sms, e := businesssms.FromMaster(pool, key)
	if e != nil {
		return nil, e
	}
	return &Store{pool: pool, aead: aead, macKey: derive("integrity"), requests: reader, accounts231: accounts, sms231: sms}, nil
}
func (s *Store) available() bool {
	return s != nil && s.pool != nil && s.aead != nil && len(s.macKey) == 32
}
func (s *Store) digest(domain string, value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	m := hmac.New(sha256.New, s.macKey)
	m.Write([]byte("goepay/admin-business83/" + domain + "/v1\x00"))
	m.Write(raw)
	return m.Sum(nil)
}
