-- Standalone OAuth profiles never reference a payment order or payment attempt.
-- Alipay SDK/web exchange the same method; preserve every historical mini claim.
INSERT INTO oauth_authorization_code_claims(provider,app_id,domain,code_hash,claim_hash)
 SELECT provider,app_id,'web',code_hash,claim_hash FROM oauth_authorization_code_claims
 WHERE provider='alipay' AND domain='mini' ON CONFLICT DO NOTHING;
CREATE TABLE profile_identity_lookup_requests (
 id text PRIMARY KEY CHECK(id~'^[0-9a-f]{64}$'),
 merchant_id bigint NOT NULL REFERENCES merchants(id),channel_id bigint NOT NULL CHECK(channel_id>0),
 merchant_revision bigint NOT NULL CHECK(merchant_revision>0),
 profile_id bigint NOT NULL CHECK(profile_id>0),profile_revision bigint NOT NULL CHECK(profile_revision>0),
 provider text NOT NULL CHECK(provider IN ('wxpay','alipay')),app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 kind text NOT NULL CHECK(kind IN ('web','mini')),path text NOT NULL,
 profile_hash bytea NOT NULL CHECK(octet_length(profile_hash)=32),
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),
 actor_username text NOT NULL,actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_auth_version bigint NOT NULL CHECK(actor_auth_version>0),
 authorization_hash bytea UNIQUE CHECK(octet_length(authorization_hash)=32),
 source_proof bytea NOT NULL CHECK(octet_length(source_proof)=32),
 state_hash bytea UNIQUE CHECK(octet_length(state_hash)=32),browser_hash bytea CHECK(octet_length(browser_hash)=32),launch_proof bytea CHECK(octet_length(launch_proof)=32),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','verified','failed','revealed')),
 subject_cipher bytea,created_at timestamptz NOT NULL DEFAULT statement_timestamp(),expires_at timestamptz NOT NULL,
 completed_at timestamptz,revealed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK(kind='web' AND path='' AND authorization_hash IS NOT NULL OR kind='mini' AND length(path) BETWEEN 1 AND 256 AND authorization_hash IS NULL),
 CHECK((state_hash IS NULL)=(launch_proof IS NULL)),CHECK(kind<>'mini' OR state_hash IS NOT NULL),
 CHECK(kind<>'mini' OR browser_hash IS NULL),CHECK(status NOT IN ('verified','revealed') OR subject_cipher IS NOT NULL),
 CHECK((status='revealed')=(revealed_at IS NOT NULL))
);
CREATE INDEX profile_identity_lookup_session ON profile_identity_lookup_requests(session_hash,expires_at);
CREATE TABLE profile_identity_lookup_events(id bigserial PRIMARY KEY,request_id text NOT NULL REFERENCES profile_identity_lookup_requests(id),operation text NOT NULL CHECK(operation IN ('created','authorized','processing','verified','failed','revealed')),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(request_id,operation));
CREATE TRIGGER profile_identity_event_immutable BEFORE UPDATE OR DELETE ON profile_identity_lookup_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_profile_identity_lookup() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.merchant_id,NEW.merchant_revision,NEW.channel_id,NEW.profile_id,NEW.profile_revision,NEW.provider,NEW.app_id,NEW.kind,NEW.path,NEW.profile_hash,NEW.session_hash,NEW.actor_username,NEW.actor_role,NEW.actor_id,NEW.actor_auth_version,NEW.authorization_hash,NEW.source_proof,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.merchant_revision,OLD.channel_id,OLD.profile_id,OLD.profile_revision,OLD.provider,OLD.app_id,OLD.kind,OLD.path,OLD.profile_hash,OLD.session_hash,OLD.actor_username,OLD.actor_role,OLD.actor_id,OLD.actor_auth_version,OLD.authorization_hash,OLD.source_proof,OLD.created_at,OLD.expires_at)
 OR OLD.state_hash IS NOT NULL AND (NEW.state_hash,NEW.browser_hash,NEW.launch_proof) IS DISTINCT FROM (OLD.state_hash,OLD.browser_hash,OLD.launch_proof)
 OR NEW.status<>OLD.status AND NOT(OLD.status='pending' AND NEW.status IN ('processing','failed') OR OLD.status='processing' AND NEW.status IN ('verified','failed') OR OLD.status='verified' AND NEW.status IN ('revealed','failed'))
 OR OLD.subject_cipher IS NOT NULL AND NEW.subject_cipher IS DISTINCT FROM OLD.subject_cipher
 OR OLD.completed_at IS NOT NULL AND NEW.completed_at IS DISTINCT FROM OLD.completed_at
 OR OLD.revealed_at IS NOT NULL AND NEW.revealed_at IS DISTINCT FROM OLD.revealed_at
 THEN RAISE EXCEPTION 'immutable standalone identity binding or result'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER profile_identity_lookup_guard BEFORE UPDATE ON profile_identity_lookup_requests FOR EACH ROW EXECUTE FUNCTION guard_profile_identity_lookup();
CREATE TRIGGER profile_identity_lookup_no_delete BEFORE DELETE ON profile_identity_lookup_requests FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
