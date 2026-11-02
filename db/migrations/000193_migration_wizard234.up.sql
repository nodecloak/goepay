CREATE TABLE migration_wizard_runs234 (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 owner_username text NOT NULL,
 owner_subject bigint NOT NULL CHECK(owner_subject>0),
 revision bigint NOT NULL CHECK(revision>0),
 status text NOT NULL CHECK(status IN ('blocked','prepared','applying','completed','failed')),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 payload_cipher bytea NOT NULL,
 archive_cipher bytea NOT NULL,
 snapshot_cipher bytea NOT NULL,
 archive_digest bytea NOT NULL CHECK(octet_length(archive_digest)=32),
 snapshot_digest bytea NOT NULL CHECK(octet_length(snapshot_digest)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK(expires_at>created_at)
);
CREATE INDEX migration_wizard_owner234 ON migration_wizard_runs234(owner_username,owner_subject,created_at DESC);
CREATE TABLE migration_wizard_events234 (
 run_id text NOT NULL REFERENCES migration_wizard_runs234(id),
 revision bigint NOT NULL CHECK(revision>0),
 status text NOT NULL CHECK(status IN ('blocked','prepared','applying','completed','failed')),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(run_id,revision)
);
CREATE TRIGGER migration_wizard_events_immutable234 BEFORE UPDATE OR DELETE ON migration_wizard_events234
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
