CREATE TABLE oauth_authorization_code_claims (
 provider text NOT NULL CHECK(provider IN ('wxpay','alipay')),
 app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 domain text NOT NULL CHECK(domain IN ('web','mini')),
 code_hash bytea NOT NULL CHECK(octet_length(code_hash)=32),
 claim_hash bytea NOT NULL CHECK(octet_length(claim_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(provider,app_id,domain,code_hash)
);
-- Preserve consumed codes without plaintext or a new HMAC algorithm. Old mini
-- claims remain separate from web codes; the original stricter table is kept.
INSERT INTO oauth_authorization_code_claims(provider,app_id,domain,code_hash,claim_hash)
 SELECT provider,app_id,CASE WHEN state_hash IS NOT NULL THEN 'web' ELSE 'mini' END,code_hash,COALESCE(state_hash,mini_state_hash)
 FROM oauth_code_claims ON CONFLICT DO NOTHING;
CREATE TRIGGER authorization_code_claim_immutable BEFORE UPDATE OR DELETE ON oauth_authorization_code_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE identity_lookup_requests (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{64}$'),
 authorization_hash bytea NOT NULL UNIQUE CHECK(octet_length(authorization_hash)=32),
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),
 actor_username text NOT NULL,actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_auth_version bigint NOT NULL CHECK(actor_auth_version>0),
 merchant_id bigint NOT NULL REFERENCES merchants(id),channel_id bigint NOT NULL CHECK(channel_id>0),
 provider text NOT NULL CHECK(provider IN ('wxpay','alipay')),app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 scope text NOT NULL,account_scope_hash bytea NOT NULL CHECK(octet_length(account_scope_hash)=32),
 purpose text NOT NULL DEFAULT 'identity-lookup' CHECK(purpose='identity-lookup'),
 browser_hash bytea CHECK(octet_length(browser_hash)=32),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','verified','failed','revealed')),
 subject_cipher bytea,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),expires_at timestamptz NOT NULL,
 completed_at timestamptz,revealed_at timestamptz,
 CHECK(provider='wxpay' AND scope='snsapi_base' OR provider='alipay' AND scope='auth_base'),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK((status IN ('verified','revealed'))=(subject_cipher IS NOT NULL)),
 CHECK((status='revealed')=(revealed_at IS NOT NULL))
);
CREATE INDEX identity_lookup_session ON identity_lookup_requests(session_hash,expires_at);
CREATE TABLE identity_lookup_states (
 state_hash bytea PRIMARY KEY CHECK(octet_length(state_hash)=32),
 request_id text NOT NULL UNIQUE REFERENCES identity_lookup_requests(id),
 purpose text NOT NULL DEFAULT 'identity-lookup' CHECK(purpose='identity-lookup'),
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),expires_at timestamptz NOT NULL,consumed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes')
);
CREATE TABLE identity_lookup_events (
 id bigserial PRIMARY KEY,request_id text NOT NULL REFERENCES identity_lookup_requests(id),
 operation text NOT NULL CHECK(operation IN ('created','authorized','processing','verified','failed','revealed')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(request_id,operation)
);
CREATE TRIGGER identity_lookup_event_immutable BEFORE UPDATE OR DELETE ON identity_lookup_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_identity_lookup() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.authorization_hash,NEW.session_hash,NEW.actor_username,NEW.actor_role,NEW.actor_id,NEW.actor_auth_version,NEW.merchant_id,NEW.channel_id,NEW.provider,NEW.app_id,NEW.scope,NEW.account_scope_hash,NEW.purpose,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM (OLD.id,OLD.authorization_hash,OLD.session_hash,OLD.actor_username,OLD.actor_role,OLD.actor_id,OLD.actor_auth_version,OLD.merchant_id,OLD.channel_id,OLD.provider,OLD.app_id,OLD.scope,OLD.account_scope_hash,OLD.purpose,OLD.created_at,OLD.expires_at)
 OR OLD.browser_hash IS NOT NULL AND NEW.browser_hash IS DISTINCT FROM OLD.browser_hash
 OR NEW.status<>OLD.status AND NOT (OLD.status='pending' AND NEW.status IN ('processing','failed') OR OLD.status='processing' AND NEW.status IN ('verified','failed') OR OLD.status='verified' AND NEW.status='revealed')
 OR OLD.subject_cipher IS NOT NULL AND NEW.subject_cipher IS DISTINCT FROM OLD.subject_cipher
 OR OLD.completed_at IS NOT NULL AND NEW.completed_at IS DISTINCT FROM OLD.completed_at
 OR OLD.revealed_at IS NOT NULL AND NEW.revealed_at IS DISTINCT FROM OLD.revealed_at
 THEN RAISE EXCEPTION 'immutable identity lookup binding or terminal result'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER identity_lookup_guard BEFORE UPDATE ON identity_lookup_requests FOR EACH ROW EXECUTE FUNCTION guard_identity_lookup();
CREATE TRIGGER identity_lookup_no_delete BEFORE DELETE ON identity_lookup_requests FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_identity_lookup_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.state_hash,NEW.request_id,NEW.purpose,NEW.browser_hash,NEW.created_at,NEW.expires_at) IS DISTINCT FROM (OLD.state_hash,OLD.request_id,OLD.purpose,OLD.browser_hash,OLD.created_at,OLD.expires_at)
 OR OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at THEN RAISE EXCEPTION 'immutable identity lookup state'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER identity_lookup_state_guard BEFORE UPDATE ON identity_lookup_states FOR EACH ROW EXECUTE FUNCTION guard_identity_lookup_state();
CREATE TRIGGER identity_lookup_state_no_delete BEFORE DELETE ON identity_lookup_states FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
