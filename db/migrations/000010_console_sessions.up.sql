ALTER TABLE console_users ADD COLUMN auth_version bigint NOT NULL DEFAULT 1;
CREATE OR REPLACE FUNCTION bump_console_auth_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.enabled IS DISTINCT FROM OLD.enabled THEN
  NEW.auth_version:=OLD.auth_version+1;
 ELSE NEW.auth_version:=OLD.auth_version; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER console_auth_version BEFORE UPDATE ON console_users FOR EACH ROW EXECUTE FUNCTION bump_console_auth_version();
CREATE TABLE console_sessions (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 username text NOT NULL, role text NOT NULL, subject_id bigint NOT NULL,
 csrf text NOT NULL, expires_at timestamptz NOT NULL, auth_version bigint NOT NULL,
 FOREIGN KEY(username,role) REFERENCES console_users(username,role) ON DELETE CASCADE
);
CREATE INDEX console_sessions_expiry ON console_sessions(expires_at);
