package migration

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type wizardRecord234 struct {
	ID, Owner                                                    string
	Subject, Revision                                            int64
	Status                                                       string
	Created, Expires                                             time.Time
	Cipher, Proof                                                []byte
	ArchiveCipher, SnapshotCipher, ArchiveDigest, SnapshotDigest []byte
}

type wizardPayload234 struct {
	Raw      []byte        `json:"raw"`
	Snapshot []byte        `json:"snapshot"`
	View     WizardView234 `json:"view"`
}

func (w *Wizard234) aead() (cipher.AEAD, error) {
	b, e := aes.NewCipher(w.key)
	if e != nil {
		return nil, ErrWizardUnavailable234
	}
	return cipher.NewGCM(b)
}
func (w *Wizard234) proof(r wizardRecord234) []byte {
	mac := hmac.New(sha256.New, w.key)
	mac.Write([]byte("goepay/migration-wizard234/record/v1\x00"))
	raw, _ := json.Marshal([]any{r.ID, r.Owner, r.Subject, r.Revision, r.Status, r.Created.UTC().Format(time.RFC3339Nano), r.Expires.UTC().Format(time.RFC3339Nano), r.ArchiveDigest, r.SnapshotDigest, r.Cipher})
	mac.Write(raw)
	return mac.Sum(nil)
}
func (w *Wizard234) encrypt(id, kind string, raw []byte) ([]byte, error) {
	aead, e := w.aead()
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return aead.Seal(nonce, nonce, raw, []byte("goepay/migration-wizard234/"+kind+"/v1/"+id)), nil
}
func (w *Wizard234) decrypt(id, kind string, raw []byte) ([]byte, error) {
	aead, e := w.aead()
	if e != nil {
		return nil, e
	}
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrWizardConflict234
	}
	plain, e := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("goepay/migration-wizard234/"+kind+"/v1/"+id))
	if e != nil {
		return nil, ErrWizardConflict234
	}
	return plain, nil
}
func (w *Wizard234) seal(r *wizardRecord234, p wizardPayload234) error {
	raw, e := json.Marshal(p.View)
	if e != nil {
		return ErrWizardInvalid234
	}
	r.Cipher, e = w.encrypt(r.ID, "view", raw)
	if e != nil {
		return e
	}
	r.Proof = w.proof(*r)
	return nil
}
func (w *Wizard234) open(r wizardRecord234) (wizardPayload234, error) {
	var p wizardPayload234
	if !hmac.Equal(w.proof(r), r.Proof) {
		return p, ErrWizardConflict234
	}
	raw, e := w.decrypt(r.ID, "view", r.Cipher)
	if e != nil || json.Unmarshal(raw, &p.View) != nil {
		return p, ErrWizardConflict234
	}
	if p.View.ID != r.ID || p.View.Status != r.Status || len(p.View.SHA256) != 64 || !p.View.ExpiresAt.Equal(r.Expires) {
		return p, ErrWizardConflict234
	}
	return p, nil
}
func (w *Wizard234) inputs(ctx context.Context, tx pgx.Tx, r *wizardRecord234, p *wizardPayload234) error {
	if e := tx.QueryRow(ctx, `SELECT archive_cipher,snapshot_cipher FROM migration_wizard_runs234 WHERE id=$1`, r.ID).Scan(&r.ArchiveCipher, &r.SnapshotCipher); e != nil {
		return e
	}
	a, s := sha256.Sum256(r.ArchiveCipher), sha256.Sum256(r.SnapshotCipher)
	if !hmac.Equal(a[:], r.ArchiveDigest) || !hmac.Equal(s[:], r.SnapshotDigest) {
		return ErrWizardConflict234
	}
	var e error
	p.Raw, e = w.decrypt(r.ID, "archive", r.ArchiveCipher)
	if e != nil {
		return e
	}
	p.Snapshot, e = w.decrypt(r.ID, "snapshot", r.SnapshotCipher)
	if e != nil {
		return e
	}
	h := sha256.Sum256(p.Raw)
	if p.View.SHA256 != hex.EncodeToString(h[:]) {
		return ErrWizardConflict234
	}
	return nil
}
func (w *Wizard234) read(ctx context.Context, tx pgx.Tx, id string, lock bool) (wizardRecord234, wizardPayload234, error) {
	var r wizardRecord234
	var p wizardPayload234
	q := `SELECT id,owner_username,owner_subject,revision,status,created_at,expires_at,payload_cipher,proof_hash,archive_digest,snapshot_digest FROM migration_wizard_runs234 WHERE id=$1`
	if lock {
		q += " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, q, id).Scan(&r.ID, &r.Owner, &r.Subject, &r.Revision, &r.Status, &r.Created, &r.Expires, &r.Cipher, &r.Proof, &r.ArchiveDigest, &r.SnapshotDigest)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, p, ErrWizardConflict234
	}
	if e != nil {
		return r, p, e
	}
	var revision int64
	var status string
	var proof []byte
	e = tx.QueryRow(ctx, `SELECT revision,status,proof_hash FROM migration_wizard_events234 WHERE run_id=$1 ORDER BY revision DESC LIMIT 1`, id).Scan(&revision, &status, &proof)
	if e != nil || revision != r.Revision || status != r.Status || !hmac.Equal(proof, r.Proof) {
		return r, p, ErrWizardConflict234
	}
	p, e = w.open(r)
	return r, p, e
}
func (w *Wizard234) save(ctx context.Context, tx pgx.Tx, r *wizardRecord234, p wizardPayload234, initial bool) error {
	if initial {
		var e error
		r.ArchiveCipher, e = w.encrypt(r.ID, "archive", p.Raw)
		if e != nil {
			return e
		}
		r.SnapshotCipher, e = w.encrypt(r.ID, "snapshot", p.Snapshot)
		if e != nil {
			return e
		}
		a, s := sha256.Sum256(r.ArchiveCipher), sha256.Sum256(r.SnapshotCipher)
		r.ArchiveDigest, r.SnapshotDigest = a[:], s[:]
	}
	if e := w.seal(r, p); e != nil {
		return e
	}
	if initial {
		if _, e := tx.Exec(ctx, `INSERT INTO migration_wizard_runs234(id,owner_username,owner_subject,revision,status,created_at,expires_at,payload_cipher,proof_hash,archive_cipher,snapshot_cipher,archive_digest,snapshot_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, r.Owner, r.Subject, r.Revision, r.Status, r.Created, r.Expires, r.Cipher, r.Proof, r.ArchiveCipher, r.SnapshotCipher, r.ArchiveDigest, r.SnapshotDigest); e != nil {
			return e
		}
	} else {
		tag, e := tx.Exec(ctx, `UPDATE migration_wizard_runs234 SET revision=$2,status=$3,payload_cipher=$4,proof_hash=$5 WHERE id=$1 AND revision=$6`, r.ID, r.Revision, r.Status, r.Cipher, r.Proof, r.Revision-1)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrWizardConflict234
		}
	}
	_, e := tx.Exec(ctx, `INSERT INTO migration_wizard_events234(run_id,revision,status,proof_hash) VALUES($1,$2,$3,$4)`, r.ID, r.Revision, r.Status, r.Proof)
	return e
}

func wizardID234(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
