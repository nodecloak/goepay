-- Diagnostic observations cannot authorize any payment, capture, release or job.
CREATE TABLE preauth_observations(
 id text PRIMARY KEY CHECK(id~'^[0-9a-f]{64}$'),client_request_no text NOT NULL CHECK(client_request_no~'^[0-9a-f]{64}$'),
 merchant_id bigint NOT NULL REFERENCES merchants(id),order_id bigint NOT NULL REFERENCES orders(id),attempt_id bigint NOT NULL REFERENCES provider_attempts(id),channel_id bigint NOT NULL CHECK(channel_id>0),
 trade_no text NOT NULL,app_id text NOT NULL,auth_no text NOT NULL,buyer_id text NOT NULL,amount bigint NOT NULL CHECK(amount>0),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),merchant_revision bigint NOT NULL CHECK(merchant_revision>0),
 session_hash bytea NOT NULL CHECK(octet_length(session_hash)=32),actor_username text NOT NULL,actor_role text NOT NULL CHECK(actor_role IN('merchant','admin')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_auth_version bigint NOT NULL CHECK(actor_auth_version>0),
 source_proof bytea NOT NULL CHECK(octet_length(source_proof)=32),expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 status text NOT NULL DEFAULT 'processing' CHECK(status IN('processing','verified','unknown')),
 rest_amount bigint,provider_status text NOT NULL DEFAULT '',order_status text NOT NULL DEFAULT '',operation_type text NOT NULL DEFAULT '',observed_at timestamptz,result_proof bytea CHECK(octet_length(result_proof)=32),
 UNIQUE(merchant_id,order_id,client_request_no),CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK((status='processing')=(observed_at IS NULL)),CHECK((status='processing')=(result_proof IS NULL)),
 CHECK(status='verified' AND rest_amount IS NOT NULL AND rest_amount BETWEEN 0 AND amount AND provider_status='SUCCESS' AND order_status IN('AUTHORIZED','FINISH') AND operation_type='FREEZE' OR status IN('processing','unknown') AND rest_amount IS NULL AND provider_status='' AND order_status='' AND operation_type='')
);
CREATE TABLE preauth_observation_events(id bigserial PRIMARY KEY,observation_id text NOT NULL REFERENCES preauth_observations(id),operation text NOT NULL CHECK(operation IN('claimed','verified','unknown')),proof bytea NOT NULL CHECK(octet_length(proof)=32),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(observation_id,operation));
CREATE TRIGGER preauth_observation_event_immutable BEFORE UPDATE OR DELETE ON preauth_observation_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_preauth_observation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'processing' OR NEW.rest_amount IS NOT NULL OR NEW.observed_at IS NOT NULL OR NEW.result_proof IS NOT NULL THEN RAISE EXCEPTION 'observation must begin as a query claim'; END IF;
  IF NOT EXISTS(SELECT 1 FROM orders o JOIN provider_attempts p ON p.order_id=o.id JOIN payment_authorizations a ON a.order_id=o.id JOIN provider_attempt_accounts ac ON ac.attempt_id=p.id WHERE o.id=NEW.order_id AND o.merchant_id=NEW.merchant_id AND o.trade_no=NEW.trade_no AND o.amount=NEW.amount AND o.type='alipay' AND p.id=NEW.attempt_id AND p.channel_id=NEW.channel_id AND p.method='preauth' AND a.channel_id=NEW.channel_id AND a.amount=NEW.amount AND a.auth_no=NEW.auth_no AND a.buyer_id=NEW.buyer_id AND ac.account_hash=NEW.account_hash) THEN RAISE EXCEPTION 'original authorization source mismatch';END IF;
 ELSE
  IF (NEW.id,NEW.client_request_no,NEW.merchant_id,NEW.order_id,NEW.attempt_id,NEW.channel_id,NEW.trade_no,NEW.app_id,NEW.auth_no,NEW.buyer_id,NEW.amount,NEW.account_hash,NEW.merchant_revision,NEW.session_hash,NEW.actor_username,NEW.actor_role,NEW.actor_id,NEW.actor_auth_version,NEW.source_proof,NEW.created_at,NEW.expires_at)
  IS DISTINCT FROM(OLD.id,OLD.client_request_no,OLD.merchant_id,OLD.order_id,OLD.attempt_id,OLD.channel_id,OLD.trade_no,OLD.app_id,OLD.auth_no,OLD.buyer_id,OLD.amount,OLD.account_hash,OLD.merchant_revision,OLD.session_hash,OLD.actor_username,OLD.actor_role,OLD.actor_id,OLD.actor_auth_version,OLD.source_proof,OLD.created_at,OLD.expires_at)
  OR OLD.status<>'processing' OR NEW.status NOT IN('verified','unknown') THEN RAISE EXCEPTION 'immutable observation source or result';END IF;
 END IF;RETURN NEW;END $$;
CREATE TRIGGER preauth_observation_guard BEFORE INSERT OR UPDATE ON preauth_observations FOR EACH ROW EXECUTE FUNCTION guard_preauth_observation();
CREATE TRIGGER preauth_observation_no_delete BEFORE DELETE ON preauth_observations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
