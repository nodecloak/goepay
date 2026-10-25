package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type CollectionGenerationInput struct {
	ExternalNo    string `json:"external_no"`
	RecipientName string `json:"recipient_name"`
	StyleID       int64  `json:"style_id,string"`
	Origin        string `json:"origin"`
	FixedAmount   int64  `json:"fixed_amount_cents,string"`
}
type CollectionGeneration struct {
	CollectionGenerationInput
	CodeID     int64  `json:"code_id,string"`
	MerchantID int64  `json:"merchant_id,string"`
	ConfigHash string `json:"config_hash"`
	Enabled    bool   `json:"enabled"`
	Replayed   bool   `json:"replayed"`
}

var collectionIntent = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// CollectionOrigin accepts a configured origin, never a client-supplied URL
// whose hostname merely resembles it. No network request or DNS lookup occurs.
func CollectionOrigin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", ErrInvalid
	}
	if u.Scheme == "http" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return "", ErrInvalid
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}
func collectionFacts(g CollectionGeneration) []any {
	return []any{g.CodeID, g.MerchantID, g.ExternalNo, g.RecipientName, g.StyleID, g.Origin, g.FixedAmount}
}
func collectionSHA(v any) []byte { b, _ := json.Marshal(v); h := sha256.Sum256(b); return h[:] }

func (s *Store) readCollectionGeneration(ctx context.Context, q fundsReader, id int64) (CollectionGeneration, error) {
	var g CollectionGeneration
	var request, config, proof, body []byte
	var owner int64
	var audited bool
	e := q.QueryRow(ctx, `SELECT g.code_id,g.merchant_id,g.external_no,g.recipient_name,g.style_id,g.origin,g.fixed_amount_cents,g.request_hash,g.config_hash,g.proof_hash,b.owner_id,b.body,EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='collection-generator' AND a.operation='generate' AND a.resource_id=g.code_id AND a.actor_role='merchant' AND a.actor_id=g.merchant_id AND a.changes->>'external_no'=g.external_no AND a.changes->>'config_hash'=encode(g.config_hash,'hex')) FROM collection_code_generations g JOIN backoffice_records b ON b.id=g.code_id AND b.resource='collection-codes' WHERE g.code_id=$1`, id).Scan(&g.CodeID, &g.MerchantID, &g.ExternalNo, &g.RecipientName, &g.StyleID, &g.Origin, &g.FixedAmount, &request, &config, &proof, &owner, &body, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	if e != nil {
		return g, e
	}
	want, e := s.referralDigest("collection-code-generation", collectionFacts(g))
	var b struct {
		Generated  bool   `json:"generated"`
		ConfigHash string `json:"config_hash"`
		Title      string `json:"title"`
		StyleID    string `json:"style_id"`
		Enabled    bool   `json:"enabled"`
		Archived   bool   `json:"archived"`
	}
	if e != nil || !audited || json.Unmarshal(body, &b) != nil || owner != g.MerchantID || !hmac.Equal(proof, want) || !hmac.Equal(config, collectionSHA(collectionFacts(g))) || !hmac.Equal(request, collectionSHA(g.CollectionGenerationInput)) || !b.Generated || b.ConfigHash != hex.EncodeToString(config) || b.Title != g.RecipientName || b.StyleID != strconv.FormatInt(g.StyleID, 10) {
		return g, ErrConflict
	}
	g.ConfigHash = hex.EncodeToString(config)
	g.Enabled = b.Enabled && !b.Archived
	return g, nil
}
func (s *Store) CollectionGeneration(ctx context.Context, id int64) (CollectionGeneration, error) {
	return s.readCollectionGeneration(ctx, s.pool, id)
}

func (s *Store) LatestCollectionGeneration(ctx context.Context, mid int64) (*CollectionGeneration, error) {
	var id int64
	e := s.pool.QueryRow(ctx, `SELECT code_id FROM collection_code_generations WHERE merchant_id=$1 ORDER BY code_id DESC LIMIT 1`, mid).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	g, e := s.CollectionGeneration(ctx, id)
	return &g, e
}

func (s *Store) GenerateCollectionCode(ctx context.Context, mid int64, in CollectionGenerationInput, configuredOrigin, actor string) (CollectionGeneration, error) {
	var out CollectionGeneration
	origin, e := CollectionOrigin(configuredOrigin)
	if e != nil || in.Origin != origin || mid < 1 || len(s.indexKey) < 32 || !collectionIntent.MatchString(in.ExternalNo) || in.RecipientName != strings.TrimSpace(in.RecipientName) || !utf8.ValidString(in.RecipientName) || utf8.RuneCountInString(in.RecipientName) < 1 || utf8.RuneCountInString(in.RecipientName) > 100 || in.StyleID < 0 || in.FixedAmount < 0 || in.FixedAmount > 100000000 {
		return out, ErrInvalid
	}
	for _, c := range in.RecipientName {
		if unicode.IsControl(c) {
			return out, ErrInvalid
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE`, mid).Scan(&enabled); e != nil {
		return out, e
	}
	if !enabled {
		return out, ErrState
	}
	var prior int64
	e = tx.QueryRow(ctx, `SELECT code_id FROM collection_code_generations WHERE merchant_id=$1 AND external_no=$2`, mid, in.ExternalNo).Scan(&prior)
	if e == nil {
		out, e = s.readCollectionGeneration(ctx, tx, prior)
		if e != nil {
			return out, e
		}
		if out.CollectionGenerationInput != in {
			return out, ErrConflict
		}
		out.Replayed = true
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if in.StyleID > 0 {
		var id int64
		if e = tx.QueryRow(ctx, `SELECT id FROM backoffice_records WHERE resource='collection-styles' AND id=$1 AND body->>'enabled'='true' FOR SHARE`, in.StyleID).Scan(&id); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return out, ErrInvalid
			}
			return out, e
		}
	}
	out = CollectionGeneration{CollectionGenerationInput: in, MerchantID: mid, Enabled: true}
	if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-codes',$1,'{}') RETURNING id`, mid).Scan(&out.CodeID); e != nil {
		return out, e
	}
	config := collectionSHA(collectionFacts(out))
	out.ConfigHash = hex.EncodeToString(config)
	proof, e := s.referralDigest("collection-code-generation", collectionFacts(out))
	if e != nil {
		return out, e
	}
	body, _ := json.Marshal(map[string]any{"generated": true, "config_hash": out.ConfigHash, "name": in.RecipientName, "title": in.RecipientName, "style_id": strconv.FormatInt(in.StyleID, 10), "enabled": true})
	if _, e = tx.Exec(ctx, `UPDATE backoffice_records SET body=$2 WHERE id=$1`, out.CodeID, body); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO collection_code_generations(code_id,merchant_id,external_no,recipient_name,style_id,origin,fixed_amount_cents,request_hash,config_hash,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, out.CodeID, mid, in.ExternalNo, in.RecipientName, in.StyleID, in.Origin, in.FixedAmount, collectionSHA(in), config, proof); e != nil {
		return out, e
	}
	changes, _ := json.Marshal(map[string]any{"external_no": in.ExternalNo, "config_hash": out.ConfigHash})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,$2,'collection-generator',$3,'generate',$4)`, mid, actor, out.CodeID, changes); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (s *Store) checkCollectionGenerators(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var tablePresent, migrationTable bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('collection_code_generations') IS NOT NULL,to_regclass('schema_migrations') IS NOT NULL`).Scan(&tablePresent, &migrationTable); err != nil {
		return err
	}
	if !tablePresent {
		var anchors int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM backoffice_records WHERE resource='collection-codes' AND body->>'generated'='true' UNION SELECT resource_id FROM backoffice_audit WHERE resource='collection-generator' AND operation='generate') sources`).Scan(&anchors); err != nil {
			return err
		}
		var migrated bool
		if migrationTable {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000086_collection_code_generations.up.sql')`).Scan(&migrated); err != nil {
				return err
			}
		}
		// Actual pre86 histories are readable. A lost new source table is never
		// accepted when an independent generation or migration anchor remains.
		report.InvalidCollectionGenerators += anchors
		if migrated && anchors == 0 {
			report.InvalidCollectionGenerators++
		}
		return nil
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT id FROM (SELECT code_id id FROM collection_code_generations UNION SELECT id FROM backoffice_records WHERE resource='collection-codes' AND body->>'generated'='true' UNION SELECT resource_id id FROM backoffice_audit WHERE resource='collection-generator' AND operation='generate') sources WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = s.readCollectionGeneration(ctx, tx, id); e != nil {
				if invalidSharingEvidence(e) {
					report.InvalidCollectionGenerators++
				} else {
					return e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			return nil
		}
	}
}

// A generated source must not silently downgrade to a legacy collection code,
// even when a mutable presentation marker is removed.
func (s *Store) validateCollectionGenerationOrder(ctx context.Context, tx pgx.Tx, id int64, in CreateOrderInput) error {
	var generated bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM collection_code_generations WHERE code_id=$1) OR EXISTS(SELECT 1 FROM backoffice_records WHERE id=$1 AND body->>'generated'='true') OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='collection-generator' AND resource_id=$1 AND operation='generate')`, id).Scan(&generated)
	if e != nil {
		return e
	}
	if !generated {
		if in.CollectionConfigHash != "" {
			return ErrConflict
		}
		return nil
	}
	g, e := s.readCollectionGeneration(ctx, tx, id)
	if e != nil {
		return e
	}
	if !g.Enabled || g.MerchantID != in.MerchantID || g.ConfigHash != in.CollectionConfigHash || g.RecipientName != in.Name || (g.FixedAmount > 0 && g.FixedAmount != in.Amount) {
		return ErrConflict
	}
	return nil
}
