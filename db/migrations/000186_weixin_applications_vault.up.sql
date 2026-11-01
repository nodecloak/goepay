CREATE TABLE runtime_weixin_app_heads (
 id bigserial PRIMARY KEY,
 revision bigint NOT NULL CHECK(revision>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE runtime_weixin_app_versions (
 app_id bigint NOT NULL REFERENCES runtime_weixin_app_heads(id),
 revision bigint NOT NULL CHECK(revision>0),
 source_channel_id bigint NOT NULL CHECK(source_channel_id>=0),
 kind text NOT NULL CHECK(kind IN ('official_account','mini_program')),
 enabled boolean NOT NULL,
 archived boolean NOT NULL,
 draft boolean NOT NULL,
 nonce bytea NOT NULL,
 ciphertext bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL,
 PRIMARY KEY(app_id,revision)
);
CREATE TABLE runtime_weixin_app_requests (
 request_id text PRIMARY KEY CHECK(request_id ~ '^[0-9a-f]{64}$'),
 actor_id bigint NOT NULL,
 app_id bigint NOT NULL,
 revision bigint NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL,
 FOREIGN KEY(app_id,revision) REFERENCES runtime_weixin_app_versions(app_id,revision)
);
CREATE FUNCTION reject_weixin_app_version_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'application versions and receipts are immutable'; END $$;
CREATE TRIGGER immutable_weixin_app_versions BEFORE UPDATE OR DELETE ON runtime_weixin_app_versions FOR EACH ROW EXECUTE FUNCTION reject_weixin_app_version_change();
CREATE TRIGGER immutable_weixin_app_requests BEFORE UPDATE OR DELETE ON runtime_weixin_app_requests FOR EACH ROW EXECUTE FUNCTION reject_weixin_app_version_change();
