package orderalias120

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
)

type aliasFacts struct {
	Version            int
	Revision           int64
	Source             payment.OrderAPITradeAliasSource120
	Alias              string
	SavedAt            time.Time
	PreviousCommitment string
}
type aliasRecord struct {
	Facts      aliasFacts
	Proof      []byte
	Configured bool
}
type aliasCommit struct {
	OrderID, MerchantID, Revision   int64
	Commitment, PreviousCommitment  string
	AuditID, ActorID                int64
	ActorUsername, BirthTransaction string
	Proof                           []byte
}

func (s *Store) commitProof(c aliasCommit) []byte {
	return s.sum("revision-witness", []any{c.OrderID, c.MerchantID, c.Revision, c.Commitment, c.PreviousCommitment, c.AuditID, c.ActorID, c.ActorUsername, c.BirthTransaction})
}

// A legitimate empty -> verified receipt observation may occur after a trusted
// pending birth. A receipt already sealed by an explicit save cannot change.
// The current view fingerprint also binds the observed receipt, so an old
// pending CAS must be reread after ordinary payment confirmation.
func sameSource(prior, current payment.OrderAPITradeAliasSource120) bool {
	if prior.VerifiedAPITradeNo != "" {
		if prior.VerifiedAPITradeNo != current.VerifiedAPITradeNo || prior.ReceiptChannelID != current.ReceiptChannelID {
			return false
		}
	} else if prior.ReceiptChannelID != 0 {
		return false
	}
	prior.ReceiptChannelID = 0
	prior.VerifiedAPITradeNo = ""
	current.ReceiptChannelID = 0
	current.VerifiedAPITradeNo = ""
	return prior == current
}
func exactFacts(raw []byte, value any) bool {
	expected, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var have, want any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&have) != nil {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(expected))
	d.UseNumber()
	return d.Decode(&want) == nil && reflect.DeepEqual(have, want)
}
func (s *Store) view(r aliasRecord, source payment.OrderAPITradeAliasSource120) View {
	fingerprint := s.sum("view", struct {
		Revision   int64
		Commitment string
		Source     payment.OrderAPITradeAliasSource120
	}{r.Facts.Revision, hex.EncodeToString(r.Proof), source})
	return View{TradeNo: source.TradeNo, MerchantID: strconv.FormatInt(source.MerchantID, 10), Alias: r.Facts.Alias, VerifiedAPITradeNo: source.VerifiedAPITradeNo, Fingerprint: hex.EncodeToString(fingerprint), CanUpdate: true}
}

func (s *Store) load(ctx context.Context, tx pgx.Tx, source payment.OrderAPITradeAliasSource120) (aliasRecord, error) {
	var r aliasRecord
	var merchant, revision int64
	var alias string
	var raw, proof []byte
	err := tx.QueryRow(ctx, `SELECT merchant_id,revision,api_trade_no_alias,facts,proof_hash FROM order_api_trade_aliases120 WHERE order_id=$1 FOR SHARE`, source.OrderID).Scan(&merchant, &revision, &alias, &raw, &proof)
	if errors.Is(err, pgx.ErrNoRows) {
		var commits, audits int64
		if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM order_api_trade_alias_commits120 WHERE order_id=$1),(SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='api-trade-alias-update')`, source.OrderID).Scan(&commits, &audits); err != nil {
			return r, err
		}
		if commits != 0 || audits != 0 {
			return r, ErrConflict
		}
		r.Facts.Source = source
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &r.Facts) != nil || !exactFacts(raw, r.Facts) || r.Facts.Version != 1 || r.Facts.Revision <= 0 || r.Facts.Revision != revision || r.Facts.SavedAt.IsZero() || merchant != source.MerchantID || r.Facts.Alias != alias || !ValidAlias(alias) || !sameSource(r.Facts.Source, source) || len(proof) != 32 {
		return aliasRecord{}, ErrConflict
	}
	r.Facts.SavedAt = r.Facts.SavedAt.UTC()
	if !hmac.Equal(s.sum("state", r.Facts), proof) {
		return aliasRecord{}, ErrConflict
	}
	r.Proof = proof
	r.Configured = true
	if err = s.history(ctx, tx, r, source); err != nil {
		return aliasRecord{}, err
	}
	return r, nil
}
func (s *Store) history(ctx context.Context, tx pgx.Tx, state aliasRecord, source payment.OrderAPITradeAliasSource120) error {
	rows, err := tx.Query(ctx, `SELECT c.order_id,c.merchant_id,c.revision,c.commitment,c.previous_commitment,c.audit_id,c.actor_id,c.actor_username,c.birth_xid::text,c.proof_hash,
 a.actor_role,a.actor_id,a.actor_name,a.resource,a.resource_id,a.operation,a.changes
 FROM order_api_trade_alias_commits120 c LEFT JOIN backoffice_audit a ON a.id=c.audit_id WHERE c.order_id=$1 ORDER BY c.revision`, source.OrderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var count int64
	previous := ""
	for rows.Next() {
		var c aliasCommit
		var role, name, resource, operation string
		var actorID, resourceID int64
		var raw []byte
		if err = rows.Scan(&c.OrderID, &c.MerchantID, &c.Revision, &c.Commitment, &c.PreviousCommitment, &c.AuditID, &c.ActorID, &c.ActorUsername, &c.BirthTransaction, &c.Proof, &role, &actorID, &name, &resource, &resourceID, &operation, &raw); err != nil {
			return err
		}
		count++
		if count > state.Facts.Revision || c.OrderID != source.OrderID || c.MerchantID != source.MerchantID || c.Revision != count || !hex64(c.Commitment) || c.PreviousCommitment != previous || c.AuditID <= 0 || c.ActorID <= 0 || c.ActorUsername == "" || c.BirthTransaction == "" || !hmac.Equal(s.commitProof(c), c.Proof) || role != "admin" || actorID != c.ActorID || name != c.ActorUsername || resource != "orders" || resourceID != source.OrderID || operation != "api-trade-alias-update" {
			return ErrConflict
		}
		expected := map[string]string{"trade_no": source.TradeNo, "merchant_id": strconv.FormatInt(source.MerchantID, 10), "revision": strconv.FormatInt(c.Revision, 10), "commitment": c.Commitment, "previous_commitment": c.PreviousCommitment}
		if !exactFacts(raw, expected) {
			return ErrConflict
		}
		if c.Revision == state.Facts.Revision && (c.Commitment != hex.EncodeToString(state.Proof) || c.PreviousCommitment != state.Facts.PreviousCommitment) {
			return ErrConflict
		}
		previous = c.Commitment
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	var audits int64
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='api-trade-alias-update'`, source.OrderID).Scan(&audits); err != nil {
		return err
	}
	if count != state.Facts.Revision || audits != count || previous != hex.EncodeToString(state.Proof) {
		return ErrConflict
	}
	return nil
}
