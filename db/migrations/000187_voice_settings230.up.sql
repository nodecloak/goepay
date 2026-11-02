-- Cloud speaker settings are an atomic, encrypted, versioned configuration.
-- No historical notification is replayed and no external execution is enabled.
CREATE TABLE voice_settings230 (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL CHECK(revision>0),
 enabled boolean NOT NULL,
 credential_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
