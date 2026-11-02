CREATE TABLE notice_accounts231 (
 kind text PRIMARY KEY CHECK(kind IN ('robot','telegram','qqbot')),
 revision bigint NOT NULL CHECK(revision>0),
 enabled boolean NOT NULL,
 credential_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TABLE qq_binding_attempts231 (
 id text PRIMARY KEY,
 revision bigint NOT NULL,
 session_hash bytea NOT NULL,
 username text NOT NULL,
 challenge_hash bytea NOT NULL,
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 event_hash bytea UNIQUE,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
