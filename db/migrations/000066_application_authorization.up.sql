CREATE TABLE application_authorization_requests (
 id text PRIMARY KEY CHECK(id~'^[0-9a-f]{64}$'),
 purpose text NOT NULL DEFAULT 'alipay_app_authorization' CHECK(purpose='alipay_app_authorization'),
 merchant_id bigint NOT NULL REFERENCES merchants(id), merchant_revision bigint NOT NULL CHECK(merchant_revision>0),
 profile_id bigint NOT NULL CHECK(profile_id>0),profile_revision bigint NOT NULL CHECK(profile_revision>0),
 issuer_app_id text NOT NULL CHECK(issuer_app_id~'^[0-9]{16}$'),expected_auth_app_id text NOT NULL CHECK(expected_auth_app_id~'^[0-9]{16}$'),
 launch_kind text NOT NULL CHECK(launch_kind IN ('basic','assigned')),app_types jsonb NOT NULL CHECK(jsonb_typeof(app_types)='array'),
 profile_hash bytea NOT NULL CHECK(octet_length(profile_hash)=32),
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),
 actor_username text NOT NULL,actor_role text NOT NULL CHECK(actor_role='admin'),actor_id bigint NOT NULL CHECK(actor_id>0),actor_auth_version bigint NOT NULL CHECK(actor_auth_version>0),
 authorization_hash bytea NOT NULL UNIQUE CHECK(octet_length(authorization_hash)=32),source_proof bytea NOT NULL CHECK(octet_length(source_proof)=32),
 state_hash bytea UNIQUE CHECK(octet_length(state_hash)=32),browser_hash bytea CHECK(octet_length(browser_hash)=32),launch_proof bytea CHECK(octet_length(launch_proof)=32),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','verified','failed','revealed')),
 result_cipher bytea,created_at timestamptz NOT NULL DEFAULT statement_timestamp(),expires_at timestamptz NOT NULL,completed_at timestamptz,revealed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK((state_hash IS NULL)=(browser_hash IS NULL) AND (state_hash IS NULL)=(launch_proof IS NULL)),
 CHECK(status NOT IN ('processing','verified','revealed') OR state_hash IS NOT NULL),
 CHECK(status NOT IN ('verified','revealed') OR result_cipher IS NOT NULL),CHECK((status='revealed')=(revealed_at IS NOT NULL)),
 CHECK(launch_kind='basic' AND app_types='[]'::jsonb OR launch_kind='assigned' AND jsonb_array_length(app_types) BETWEEN 1 AND 5)
);
CREATE INDEX application_authorization_session ON application_authorization_requests(session_hash,expires_at);
CREATE TABLE application_authorization_events(id bigserial PRIMARY KEY,request_id text NOT NULL REFERENCES application_authorization_requests(id),operation text NOT NULL CHECK(operation IN ('created','authorized','processing','verified','failed','revealed')),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(request_id,operation));
CREATE TRIGGER application_authorization_event_immutable BEFORE UPDATE OR DELETE ON application_authorization_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
-- This realm is specifically alipay.open.auth.token.app. Ordinary user OAuth code
-- claims remain separate; neither table is consulted as a fallback protocol.
CREATE TABLE application_authorization_code_claims(issuer_app_id text NOT NULL CHECK(issuer_app_id~'^[0-9]{16}$'),code_hash bytea NOT NULL CHECK(octet_length(code_hash)=32),request_id text NOT NULL REFERENCES application_authorization_requests(id),claim_proof bytea NOT NULL CHECK(octet_length(claim_proof)=32),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(issuer_app_id,code_hash),UNIQUE(request_id));
CREATE TRIGGER application_authorization_claim_immutable BEFORE UPDATE OR DELETE ON application_authorization_code_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_application_authorization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.purpose,NEW.merchant_id,NEW.merchant_revision,NEW.profile_id,NEW.profile_revision,NEW.issuer_app_id,NEW.expected_auth_app_id,NEW.launch_kind,NEW.app_types,NEW.profile_hash,NEW.session_hash,NEW.actor_username,NEW.actor_role,NEW.actor_id,NEW.actor_auth_version,NEW.authorization_hash,NEW.source_proof,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM (OLD.id,OLD.purpose,OLD.merchant_id,OLD.merchant_revision,OLD.profile_id,OLD.profile_revision,OLD.issuer_app_id,OLD.expected_auth_app_id,OLD.launch_kind,OLD.app_types,OLD.profile_hash,OLD.session_hash,OLD.actor_username,OLD.actor_role,OLD.actor_id,OLD.actor_auth_version,OLD.authorization_hash,OLD.source_proof,OLD.created_at,OLD.expires_at)
 OR OLD.state_hash IS NOT NULL AND (NEW.state_hash,NEW.browser_hash,NEW.launch_proof) IS DISTINCT FROM (OLD.state_hash,OLD.browser_hash,OLD.launch_proof)
 OR OLD.state_hash IS NULL AND NEW.state_hash IS NOT NULL AND (OLD.status<>'pending' OR NEW.status<>'pending')
 OR NEW.status<>OLD.status AND NOT(OLD.status='pending' AND NEW.status IN ('processing','failed') OR OLD.status='processing' AND NEW.status IN ('verified','failed') OR OLD.status='verified' AND NEW.status IN ('revealed','failed'))
 OR NEW.result_cipher IS DISTINCT FROM OLD.result_cipher AND NOT(OLD.status='processing' AND NEW.status='verified' AND OLD.result_cipher IS NULL AND NEW.result_cipher IS NOT NULL)
 OR OLD.completed_at IS NOT NULL AND NEW.completed_at IS DISTINCT FROM OLD.completed_at
 OR OLD.revealed_at IS NOT NULL AND NEW.revealed_at IS DISTINCT FROM OLD.revealed_at
 THEN RAISE EXCEPTION 'immutable application authorization binding or result'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER application_authorization_guard BEFORE UPDATE ON application_authorization_requests FOR EACH ROW EXECUTE FUNCTION guard_application_authorization();
CREATE TRIGGER application_authorization_no_delete BEFORE DELETE ON application_authorization_requests FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
