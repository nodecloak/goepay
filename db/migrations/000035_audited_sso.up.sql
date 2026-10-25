CREATE TABLE console_sso_grants (
 grant_hash bytea PRIMARY KEY CHECK(octet_length(grant_hash)=32),
 issuer_session_hash bytea NOT NULL CHECK(octet_length(issuer_session_hash)=32),
 issuer_username text NOT NULL,issuer_id bigint NOT NULL,issuer_auth_version bigint NOT NULL,
 target_username text NOT NULL,target_id bigint NOT NULL REFERENCES merchants(id),target_auth_version bigint NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT statement_timestamp()+interval '60 seconds',
 consumed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '60 seconds')
);
CREATE INDEX console_sso_grants_expiry ON console_sso_grants(expires_at);
CREATE TABLE console_session_issuers (
 token_hash bytea PRIMARY KEY REFERENCES console_sessions(token_hash) ON DELETE CASCADE,
 issuer_session_hash bytea NOT NULL CHECK(octet_length(issuer_session_hash)=32),
 issuer_username text NOT NULL,issuer_id bigint NOT NULL,issuer_auth_version bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp()
);
-- The issuer's session intentionally has no cascading FK: its deletion must leave
-- this association present so lookup rejects the delegated session, not turn it
-- into an ordinary merchant session by deleting only its issuer metadata.
