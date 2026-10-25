CREATE TABLE admin_funds_credentials (
 username text PRIMARY KEY,
 role text NOT NULL DEFAULT 'admin' CHECK(role='admin'),
 password_hash text NOT NULL CHECK(password_hash LIKE '$argon2id$%'),
 credential_epoch bigint NOT NULL DEFAULT 1 CHECK(credential_epoch>0),
 enabled boolean NOT NULL DEFAULT true,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(username,role) REFERENCES console_users(username,role) ON DELETE CASCADE
);
CREATE FUNCTION bump_admin_funds_epoch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash OR NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.credential_epoch>OLD.credential_epoch THEN
  NEW.credential_epoch:=OLD.credential_epoch+1;
 ELSE NEW.credential_epoch:=OLD.credential_epoch; END IF;
 NEW.updated_at:=clock_timestamp(); RETURN NEW;
END $$;
CREATE TRIGGER admin_funds_epoch BEFORE UPDATE ON admin_funds_credentials FOR EACH ROW EXECUTE FUNCTION bump_admin_funds_epoch();
CREATE TABLE admin_funds_challenges (
 challenge_hash bytea PRIMARY KEY CHECK(octet_length(challenge_hash)=32),
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),
 username text NOT NULL REFERENCES admin_funds_credentials(username),
 subject_id bigint NOT NULL,auth_version bigint NOT NULL,credential_epoch bigint NOT NULL,
 action_kind text NOT NULL,resource_id text NOT NULL,action_digest bytea NOT NULL CHECK(octet_length(action_digest)=32),
 created_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 confirmed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '2 minutes')
);
CREATE INDEX admin_funds_challenge_expiry ON admin_funds_challenges(expires_at);
CREATE TABLE admin_funds_grants (
 grant_hash bytea PRIMARY KEY CHECK(octet_length(grant_hash)=32),
 challenge_hash bytea NOT NULL UNIQUE REFERENCES admin_funds_challenges(challenge_hash),
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),
 username text NOT NULL REFERENCES admin_funds_credentials(username),
 subject_id bigint NOT NULL,auth_version bigint NOT NULL,credential_epoch bigint NOT NULL,
 action_kind text NOT NULL,resource_id text NOT NULL,action_digest bytea NOT NULL CHECK(octet_length(action_digest)=32),
 created_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 consumed_at timestamptz,operation_key text,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK((consumed_at IS NULL)=(operation_key IS NULL))
);
CREATE INDEX admin_funds_grant_expiry ON admin_funds_grants(expires_at);
CREATE TABLE admin_funds_throttle (
 key_hash bytea PRIMARY KEY CHECK(octet_length(key_hash)=32),
 window_start timestamptz NOT NULL,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 failures integer NOT NULL DEFAULT 0 CHECK(failures>=0),
 updated_at timestamptz NOT NULL
);
CREATE TABLE admin_funds_audit (
 id bigserial PRIMARY KEY,
 username text NOT NULL,subject_id bigint NOT NULL,
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),
 operation text NOT NULL,
 action_kind text NOT NULL DEFAULT '',resource_id text NOT NULL DEFAULT '',
 action_digest bytea,operation_key text NOT NULL DEFAULT '',
 credential_epoch bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX admin_funds_audit_actor ON admin_funds_audit(username,id DESC);
CREATE TRIGGER admin_funds_audit_immutable BEFORE UPDATE OR DELETE ON admin_funds_audit FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
