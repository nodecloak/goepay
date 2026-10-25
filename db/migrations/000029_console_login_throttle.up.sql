-- Shared bounded counters. Key material is a domain-separated hash, never a
-- password, credential, cookie, or plaintext username/address.
CREATE TABLE console_login_throttle (
 key_hash bytea PRIMARY KEY CHECK(octet_length(key_hash)=32),
 window_start timestamptz NOT NULL,
 attempts integer NOT NULL CHECK(attempts>0),
 updated_at timestamptz NOT NULL DEFAULT statement_timestamp()
);
CREATE INDEX console_login_throttle_expiry ON console_login_throttle(updated_at);
