CREATE TABLE kyc_settings233 (
 id smallint PRIMARY KEY CHECK(id=1),
 revision bigint NOT NULL CHECK(revision>0),
 config_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TABLE kyc_attempts233 (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 revision bigint NOT NULL CHECK(revision>0),
 status text NOT NULL CHECK(status IN ('ready','initializing','pending','unknown','verified','rejected','passed','expired','cancelled')),
 attempt_cipher bytea NOT NULL,
 next_query_at timestamptz NOT NULL DEFAULT 'epoch',
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE UNIQUE INDEX kyc_attempt_active233 ON kyc_attempts233(merchant_id)
 WHERE status IN ('ready','initializing','pending','unknown','verified');
CREATE TABLE kyc_attempt_events233 (
 attempt_id text NOT NULL REFERENCES kyc_attempts233(id),
 revision bigint NOT NULL CHECK(revision>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 status text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(attempt_id,revision)
);
CREATE TRIGGER kyc_attempt_events_immutable233 BEFORE UPDATE OR DELETE ON kyc_attempt_events233
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_certifications233 (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),
 attempt_id text UNIQUE NOT NULL REFERENCES kyc_attempts233(id),
 cert_type smallint NOT NULL CHECK(cert_type IN (0,1)),
 certified_at timestamptz NOT NULL,
 record_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TABLE ocr_operations233 (
 operation_id text PRIMARY KEY CHECK(operation_id ~ '^[0-9a-f]{32}$'),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),
 actor_id bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ocr_actor_rate233 ON ocr_operations233(actor_role,actor_id,created_at);
CREATE TABLE kyc_certification_events233 (
 event_id bigserial UNIQUE NOT NULL,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 attempt_id text UNIQUE NOT NULL REFERENCES kyc_attempts233(id),
 certified_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(merchant_id,attempt_id)
);
CREATE TRIGGER kyc_certification_events_immutable233 BEFORE UPDATE OR DELETE ON kyc_certification_events233
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
