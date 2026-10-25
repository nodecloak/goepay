CREATE TABLE oauth_payment_intents (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 provider text NOT NULL CHECK(provider IN ('wxpay','alipay')),
 app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 method text NOT NULL CHECK(method='jsapi'),
 purpose text NOT NULL CHECK(purpose='payment-jsapi')
);
CREATE TABLE oauth_states (
 state_hash bytea PRIMARY KEY CHECK(octet_length(state_hash)=32),
 order_id bigint NOT NULL REFERENCES oauth_payment_intents(order_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 channel_id bigint NOT NULL,
 provider text NOT NULL,
 app_id text NOT NULL,
 method text NOT NULL,
 purpose text NOT NULL,
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '5 minutes',
 consumed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes')
);
CREATE INDEX oauth_states_order ON oauth_states(order_id,expires_at);
CREATE TABLE oauth_code_claims (
 provider text NOT NULL,
 app_id text NOT NULL,
 code_hash bytea NOT NULL CHECK(octet_length(code_hash)=32),
 state_hash bytea NOT NULL UNIQUE REFERENCES oauth_states(state_hash),
 PRIMARY KEY(provider,app_id,code_hash)
);
CREATE FUNCTION guard_oauth_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'OAuth payment identity is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER oauth_intent_guard BEFORE UPDATE ON oauth_payment_intents FOR EACH ROW EXECUTE FUNCTION guard_oauth_binding();
CREATE TRIGGER oauth_intent_no_delete BEFORE DELETE ON oauth_payment_intents FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER oauth_code_no_update BEFORE UPDATE OR DELETE ON oauth_code_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_oauth_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.state_hash,NEW.order_id,NEW.merchant_id,NEW.channel_id,NEW.provider,NEW.app_id,NEW.method,NEW.purpose,NEW.browser_hash,NEW.created_at,NEW.expires_at) IS DISTINCT FROM (OLD.state_hash,OLD.order_id,OLD.merchant_id,OLD.channel_id,OLD.provider,OLD.app_id,OLD.method,OLD.purpose,OLD.browser_hash,OLD.created_at,OLD.expires_at) OR OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at THEN RAISE EXCEPTION 'OAuth state binding is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER oauth_state_guard BEFORE UPDATE ON oauth_states FOR EACH ROW EXECUTE FUNCTION guard_oauth_state();
CREATE TRIGGER oauth_state_no_delete BEFORE DELETE ON oauth_states FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
