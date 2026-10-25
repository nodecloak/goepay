CREATE TABLE mini_payment_intents (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 provider text NOT NULL CHECK(provider IN ('wxpay','alipay')),
 app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 method text NOT NULL,
 purpose text NOT NULL CHECK(purpose='mini-payment'),
 path text NOT NULL CHECK(length(path) BETWEEN 1 AND 256),
 CHECK(provider='wxpay' AND method='jsapi' OR provider='alipay' AND method='mini'),
 UNIQUE(order_id,channel_id,provider,app_id,method,purpose,path)
);
CREATE TABLE mini_oauth_states (
 state_hash bytea PRIMARY KEY CHECK(octet_length(state_hash)=32),
 order_id bigint NOT NULL REFERENCES mini_payment_intents(order_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 channel_id bigint NOT NULL,
 provider text NOT NULL,
 app_id text NOT NULL,
 method text NOT NULL,
 purpose text NOT NULL,
 path text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT statement_timestamp()+interval '5 minutes',
 consumed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 UNIQUE(state_hash,provider,app_id),
 FOREIGN KEY(order_id,channel_id,provider,app_id,method,purpose,path) REFERENCES mini_payment_intents(order_id,channel_id,provider,app_id,method,purpose,path)
);
CREATE INDEX mini_oauth_states_order ON mini_oauth_states(order_id,expires_at);
ALTER TABLE oauth_code_claims ALTER COLUMN state_hash DROP NOT NULL;
ALTER TABLE oauth_code_claims ADD COLUMN mini_state_hash bytea UNIQUE;
ALTER TABLE oauth_code_claims ADD CONSTRAINT oauth_code_mini_state_binding FOREIGN KEY(mini_state_hash,provider,app_id) REFERENCES mini_oauth_states(state_hash,provider,app_id);
ALTER TABLE oauth_code_claims ADD CONSTRAINT oauth_code_exactly_one_state CHECK(num_nonnulls(state_hash,mini_state_hash)=1);
CREATE TRIGGER mini_intent_guard BEFORE UPDATE ON mini_payment_intents FOR EACH ROW EXECUTE FUNCTION guard_oauth_binding();
CREATE TRIGGER mini_intent_no_delete BEFORE DELETE ON mini_payment_intents FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_mini_oauth_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.state_hash,NEW.order_id,NEW.merchant_id,NEW.channel_id,NEW.provider,NEW.app_id,NEW.method,NEW.purpose,NEW.path,NEW.created_at,NEW.expires_at) IS DISTINCT FROM (OLD.state_hash,OLD.order_id,OLD.merchant_id,OLD.channel_id,OLD.provider,OLD.app_id,OLD.method,OLD.purpose,OLD.path,OLD.created_at,OLD.expires_at) OR OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at THEN RAISE EXCEPTION 'Mini OAuth state binding is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mini_state_guard BEFORE UPDATE ON mini_oauth_states FOR EACH ROW EXECUTE FUNCTION guard_mini_oauth_state();
CREATE TRIGGER mini_state_no_delete BEFORE DELETE ON mini_oauth_states FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

