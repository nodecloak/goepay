-- 中转代理 (original admin/set.php mod=proxy) secrets. The proxy password
-- (proxy_pwd) and proxy-API key (proxy_apikey) are NOT plaintext settings:
-- they are stored AEAD-encrypted here, write-only, in a single versioned row,
-- exactly like captcha_credentials203. Everything else (mode, server, port,
-- user, protocol, API url) stays an ordinary backoffice setting.
CREATE TABLE proxy_credentials (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL CHECK(revision>0),
 pwd_cipher bytea NOT NULL,
 apikey_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
