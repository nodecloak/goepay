CREATE TABLE enrollment_challenges (
 id text PRIMARY KEY CHECK(length(id)=43),
 purpose text NOT NULL CHECK(purpose IN ('register','recover')),
 email_hash bytea NOT NULL CHECK(octet_length(email_hash)=32),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 code_hash bytea NOT NULL CHECK(octet_length(code_hash)=32),
 target_merchant_id bigint REFERENCES merchants(id),
 target_auth_version bigint CHECK(target_auth_version>0),
 state text NOT NULL CHECK(state IN ('sending','sent','failed')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 CHECK(expires_at>created_at),
 CHECK((target_merchant_id IS NULL)=(target_auth_version IS NULL))
);
CREATE INDEX enrollment_email_recent ON enrollment_challenges(email_hash,created_at DESC);
CREATE INDEX enrollment_challenge_expiry ON enrollment_challenges(expires_at);
CREATE TABLE enrollment_limits (
 key_hash bytea PRIMARY KEY CHECK(octet_length(key_hash)=32),
 window_start timestamptz NOT NULL,
 attempts integer NOT NULL CHECK(attempts>0)
);
CREATE TABLE enrollment_emails (
 email text PRIMARY KEY CHECK(email=lower(email)),
 email_hash bytea UNIQUE NOT NULL CHECK(octet_length(email_hash)=32),
 merchant_id bigint UNIQUE NOT NULL REFERENCES merchants(id),
 username text NOT NULL,
 role text NOT NULL DEFAULT 'merchant' CHECK(role='merchant'),
 verified_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 FOREIGN KEY(username,role) REFERENCES console_users(username,role)
);
-- Email claims are established by a completed verification, never editable
-- profile data. A future change-email flow needs an explicit evidence lifecycle.
CREATE TRIGGER enrollment_email_immutable BEFORE UPDATE OR DELETE ON enrollment_emails FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE enrollment_invitation_uses (
 invitation_id bigint NOT NULL REFERENCES backoffice_records(id),
 merchant_id bigint UNIQUE NOT NULL REFERENCES merchants(id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 PRIMARY KEY(invitation_id,merchant_id)
);
CREATE TRIGGER enrollment_invitation_immutable BEFORE UPDATE OR DELETE ON enrollment_invitation_uses FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
