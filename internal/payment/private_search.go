package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/privatesearch"
)

var ErrPrivateSearchBroad = errors.New("private search too broad; narrow account or name")

type privateSearchSource struct {
	Kind           string
	ID, MerchantID int64
	Cipher         []byte
	Fields         privatesearch.Fields
}
type privateSearchReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func privateSearchSchema(ctx context.Context, q privateSearchReader) (bool, error) {
	var yes bool
	e := q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.private_search_projections') IS NOT NULL`).Scan(&yes)
	return yes, e
}
func (s *Store) indexPrivateSearchTx(ctx context.Context, tx pgx.Tx, kind string, id, mid int64, cipher []byte, fields privatesearch.Fields) error {
	// Authentic old-schema upgrade fixtures may still create original sources.
	// Search itself always requires migration92 and complete projections.
	yes, e := privateSearchSchema(ctx, tx)
	if e != nil || !yes {
		return e
	}
	terms, e := privatesearch.Build(s.indexKey, fields)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(cipher)
	proof, e := privatesearch.Proof(s.indexKey, kind, id, mid, hash[:], terms)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO private_search_projections(source_kind,source_id,merchant_id,source_hash,account_terms,name_terms,remark_terms,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(source_kind,source_id) DO NOTHING`, kind, id, mid, hash[:], terms[privatesearch.Account], terms[privatesearch.Name], terms[privatesearch.Remark], proof)
	return e
}

func (s *Store) readPrivateSearchSource(ctx context.Context, q privateSearchReader, kind string, id int64) (privateSearchSource, error) {
	out := privateSearchSource{Kind: kind, ID: id}
	switch kind {
	case "settlement_config":
		var c recipientRecord
		e := q.QueryRow(ctx, `SELECT id,merchant_id,recipient_type,app_id,identity_hash,recipient_cipher FROM settlement_recipient_configs WHERE id=$1`, id).Scan(&c.ID, &c.MerchantID, &c.Type, &c.AppID, &c.Hash, &c.Cipher)
		if e != nil {
			return out, e
		}
		in, e := s.decodeSettlementConfig(c, c.MerchantID)
		if e != nil {
			return out, e
		}
		out.MerchantID = c.MerchantID
		out.Cipher = c.Cipher
		out.Fields = privatesearch.Fields{privatesearch.Account: in.Account, privatesearch.Name: in.Name}
	case "transfer_intent":
		var original []byte
		e := q.QueryRow(ctx, `SELECT t.merchant_id,a.recipient_cipher,p.request_hash FROM transfers t JOIN provider_transfer_attempts a ON a.transfer_id=t.id LEFT JOIN provider_transfer_policies p ON p.transfer_id=t.id WHERE t.id=$1`, id).Scan(&out.MerchantID, &out.Cipher, &original)
		if e != nil {
			return out, e
		}
		in, e := s.decryptTransfer(id, out.Cipher)
		if e != nil {
			return out, e
		}
		if len(original) > 0 {
			_, request, e := s.transferPolicyHashes(out.MerchantID, in)
			if e != nil || !hmac.Equal(original, request) {
				return out, ErrConflict
			}
		}
		out.Fields = privatesearch.Fields{privatesearch.Account: in.Account, privatesearch.Name: in.Name, privatesearch.Remark: in.Remark}
	default:
		return out, ErrInvalid
	}
	return out, nil
}

// RebuildPrivateSearch derives only missing projections from authenticated
// original encrypted payloads. Run before accepting requests after migration;
// GET queries never backfill. Each bounded batch commits independently.
func (s *Store) RebuildPrivateSearch(ctx context.Context) error {
	for _, kind := range []string{"settlement_config", "transfer_intent"} {
		for {
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(158092)`); e != nil {
				tx.Rollback(ctx)
				return e
			}
			yes, e := privateSearchSchema(ctx, tx)
			if e != nil || !yes {
				tx.Rollback(ctx)
				if e != nil {
					return e
				}
				return ErrState
			}
			source := `SELECT c.id FROM settlement_recipient_configs c WHERE NOT EXISTS(SELECT 1 FROM private_search_projections p WHERE p.source_kind='settlement_config' AND p.source_id=c.id) ORDER BY c.id LIMIT 250`
			if kind == "transfer_intent" {
				source = `SELECT a.transfer_id FROM provider_transfer_attempts a WHERE NOT EXISTS(SELECT 1 FROM private_search_projections p WHERE p.source_kind='transfer_intent' AND p.source_id=a.transfer_id) ORDER BY a.transfer_id LIMIT 250`
			}
			rows, e := tx.Query(ctx, source)
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			ids := []int64{}
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					break
				}
				ids = append(ids, id)
			}
			rows.Close()
			if e == nil {
				e = rows.Err()
			}
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			for _, id := range ids {
				v, err := s.readPrivateSearchSource(ctx, tx, kind, id)
				if err != nil {
					e = err
					break
				}
				if err = s.indexPrivateSearchTx(ctx, tx, kind, id, v.MerchantID, v.Cipher, v.Fields); err != nil {
					e = err
					break
				}
			}
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			if e = tx.Commit(ctx); e != nil {
				return e
			}
			if len(ids) < 250 {
				break
			}
		}
	}
	return s.verifyPrivateSearchProjections(ctx)
}

// PrivateSearchIDs returns original config/transfer identifiers, not plaintext.
// Every candidate must verify against both the encrypted source and substring.
// The caller independently binds its authenticated merchant and list filters.
func (s *Store) PrivateSearchIDs(ctx context.Context, q privateSearchReader, kind string, mid int64, fields []privatesearch.Field, raw string) ([]int64, error) {
	if q == nil || mid < 0 || len(fields) < 1 || len(fields) > 2 || kind != "settlement_config" && kind != "transfer_intent" {
		return nil, ErrInvalid
	}
	yes, e := privateSearchSchema(ctx, q)
	if e != nil {
		return nil, e
	}
	if !yes {
		return nil, ErrState
	}
	source := `SELECT 1 FROM settlement_recipient_configs c WHERE ($1::bigint=0 OR c.merchant_id=$1) AND NOT EXISTS(SELECT 1 FROM private_search_projections p WHERE p.source_kind='settlement_config' AND p.source_id=c.id)`
	if kind == "transfer_intent" {
		source = `SELECT 1 FROM provider_transfer_attempts a JOIN transfers t ON t.id=a.transfer_id WHERE ($1::bigint=0 OR t.merchant_id=$1) AND NOT EXISTS(SELECT 1 FROM private_search_projections p WHERE p.source_kind='transfer_intent' AND p.source_id=a.transfer_id)`
	}
	var missing bool
	if e = q.QueryRow(ctx, "SELECT EXISTS("+source+")", mid).Scan(&missing); e != nil {
		return nil, e
	}
	if missing {
		return nil, ErrState
	}
	args := []any{kind, mid}
	where := ""
	for i, f := range fields {
		terms, e := privatesearch.Query(s.indexKey, f, raw)
		if e != nil {
			return nil, ErrInvalid
		}
		column := map[privatesearch.Field]string{privatesearch.Account: "account_terms", privatesearch.Name: "name_terms", privatesearch.Remark: "remark_terms"}[f]
		if column == "" {
			return nil, ErrInvalid
		}
		if i > 0 {
			where += " OR "
		}
		args = append(args, terms[f])
		where += fmt.Sprintf("%s @> $%d::text[]", column, len(args))
	}
	rows, e := q.Query(ctx, `SELECT source_id,merchant_id,source_hash,account_terms,name_terms,remark_terms,proof_hash FROM private_search_projections WHERE source_kind=$1 AND ($2::bigint=0 OR merchant_id=$2) AND (`+where+`) ORDER BY source_id LIMIT 2001`, args...)
	if e != nil {
		return nil, e
	}
	type candidate struct {
		id, mid     int64
		hash, proof []byte
		terms       privatesearch.Terms
	}
	candidates := []candidate{}
	for rows.Next() {
		c := candidate{terms: privatesearch.Terms{}}
		var account, name, remark []string
		if e = rows.Scan(&c.id, &c.mid, &c.hash, &account, &name, &remark, &c.proof); e != nil {
			break
		}
		c.terms[privatesearch.Account] = account
		c.terms[privatesearch.Name] = name
		c.terms[privatesearch.Remark] = remark
		candidates = append(candidates, c)
	}
	rows.Close()
	if e == nil {
		e = rows.Err()
	}
	if e != nil {
		return nil, e
	}
	if len(candidates) > 2000 {
		return nil, ErrPrivateSearchBroad
	}
	candidateIDs := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		candidateIDs = append(candidateIDs, c.id)
	}
	originals, e := s.privateSearchSources(ctx, q, kind, candidateIDs)
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for _, c := range candidates {
		if !privatesearch.Verify(s.indexKey, kind, c.id, c.mid, c.hash, c.terms, c.proof) {
			return nil, ErrConflict
		}
		v, ok := originals[c.id]
		if !ok {
			return nil, ErrConflict
		}
		hash := sha256.Sum256(v.Cipher)
		if v.MerchantID != c.mid || !hmac.Equal(hash[:], c.hash) {
			return nil, ErrConflict
		}
		for _, f := range fields {
			if privatesearch.Match(v.Fields, f, raw) {
				ids = append(ids, c.id)
				break
			}
		}
	}
	return ids, nil
}

func (s *Store) SearchPrivateTransfers(ctx context.Context, mid int64, field privatesearch.Field, raw string) ([]int64, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	ids, e := s.PrivateSearchIDs(ctx, tx, "transfer_intent", mid, []privatesearch.Field{field}, raw)
	if e != nil {
		return nil, e
	}
	return ids, tx.Commit(ctx)
}

// Load candidate originals in one bounded query, rather than one database
// round trip per result. Decryption and identity checks remain in the domain.
func (s *Store) privateSearchSources(ctx context.Context, q privateSearchReader, kind string, ids []int64) (map[int64]privateSearchSource, error) {
	out := map[int64]privateSearchSource{}
	if len(ids) == 0 {
		return out, nil
	}
	sql := `SELECT id,merchant_id,recipient_type,app_id,identity_hash,recipient_cipher,NULL::bytea FROM settlement_recipient_configs WHERE id=ANY($1::bigint[])`
	if kind == "transfer_intent" {
		sql = `SELECT t.id,t.merchant_id,''::text,''::text,NULL::bytea,a.recipient_cipher,p.request_hash FROM transfers t JOIN provider_transfer_attempts a ON a.transfer_id=t.id LEFT JOIN provider_transfer_policies p ON p.transfer_id=t.id WHERE t.id=ANY($1::bigint[])`
	}
	rows, e := q.Query(ctx, sql, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var c recipientRecord
		var expected []byte
		if e = rows.Scan(&c.ID, &c.MerchantID, &c.Type, &c.AppID, &c.Hash, &c.Cipher, &expected); e != nil {
			return nil, e
		}
		v := privateSearchSource{Kind: kind, ID: c.ID, MerchantID: c.MerchantID, Cipher: c.Cipher}
		if kind == "settlement_config" {
			in, e := s.decodeSettlementConfig(c, c.MerchantID)
			if e != nil {
				return nil, e
			}
			v.Fields = privatesearch.Fields{privatesearch.Account: in.Account, privatesearch.Name: in.Name}
		} else {
			in, e := s.decryptTransfer(c.ID, c.Cipher)
			if e != nil {
				return nil, e
			}
			if len(expected) > 0 {
				_, actual, e := s.transferPolicyHashes(c.MerchantID, in)
				if e != nil || !hmac.Equal(expected, actual) {
					return nil, ErrConflict
				}
			}
			v.Fields = privatesearch.Fields{privatesearch.Account: in.Account, privatesearch.Name: in.Name, privatesearch.Remark: in.Remark}
		}
		out[v.ID] = v
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(out) != len(ids) {
		return nil, ErrConflict
	}
	return out, nil
}

// Validate every existing projection before API startup as well as newly
// rebuilt rows. Corruption is not silently repaired into a trusted new proof.
func (s *Store) verifyPrivateSearchProjections(ctx context.Context) error {
	for _, kind := range []string{"settlement_config", "transfer_intent"} {
		var after int64
		for {
			tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if e != nil {
				return e
			}
			rows, e := tx.Query(ctx, `SELECT source_id,merchant_id,source_hash,account_terms,name_terms,remark_terms,proof_hash FROM private_search_projections WHERE source_kind=$1 AND source_id>$2 ORDER BY source_id LIMIT 250`, kind, after)
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			type projection struct {
				id, mid     int64
				hash, proof []byte
				terms       privatesearch.Terms
			}
			all := []projection{}
			ids := []int64{}
			for rows.Next() {
				p := projection{terms: privatesearch.Terms{}}
				var a, n, r []string
				if e = rows.Scan(&p.id, &p.mid, &p.hash, &a, &n, &r, &p.proof); e != nil {
					break
				}
				p.terms[privatesearch.Account] = a
				p.terms[privatesearch.Name] = n
				p.terms[privatesearch.Remark] = r
				all = append(all, p)
				ids = append(ids, p.id)
			}
			rows.Close()
			if e == nil {
				e = rows.Err()
			}
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			originals, e := s.privateSearchSources(ctx, tx, kind, ids)
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			for _, p := range all {
				v, ok := originals[p.id]
				hash := sha256.Sum256(v.Cipher)
				if !ok || v.MerchantID != p.mid || !hmac.Equal(p.hash, hash[:]) || !privatesearch.Verify(s.indexKey, kind, p.id, p.mid, p.hash, p.terms, p.proof) {
					e = ErrConflict
					break
				}
				after = p.id
			}
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			if e = tx.Commit(ctx); e != nil {
				return e
			}
			if len(all) < 250 {
				break
			}
		}
	}
	return nil
}
