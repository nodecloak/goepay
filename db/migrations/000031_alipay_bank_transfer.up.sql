ALTER TABLE bank_transfer_lifecycle ADD COLUMN provider_kind text NOT NULL DEFAULT 'wxpay_v2' CHECK(provider_kind IN ('wxpay_v2','alipay_bankcard'));
CREATE OR REPLACE FUNCTION bank_provider_kind_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.provider_kind<>OLD.provider_kind THEN RAISE EXCEPTION 'immutable bank transfer provider'; END IF; RETURN NEW; END $$;
CREATE TRIGGER bank_provider_kind_immutable BEFORE UPDATE ON bank_transfer_lifecycle FOR EACH ROW EXECUTE FUNCTION bank_provider_kind_guard();

CREATE TABLE alipay_bank_bindings (
 transfer_id bigint PRIMARY KEY REFERENCES bank_transfer_lifecycle(transfer_id),
 product_code text NOT NULL DEFAULT 'TRANS_BANKCARD_NO_PWD' CHECK(product_code='TRANS_BANKCARD_NO_PWD'),
 request_digest bytea NOT NULL CHECK(octet_length(request_digest)=32),
 submit_order_id text NOT NULL DEFAULT '',submit_bound boolean NOT NULL DEFAULT false,
 estimated_fee_amount bigint CHECK(estimated_fee_amount>=0),
 CHECK(NOT submit_bound OR submit_order_id<>''),created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION alipay_bank_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF (NEW.transfer_id,NEW.product_code,NEW.request_digest) IS DISTINCT FROM (OLD.transfer_id,OLD.product_code,OLD.request_digest) OR (OLD.submit_order_id<>'' AND NEW.submit_order_id<>OLD.submit_order_id) OR (OLD.submit_bound AND NOT NEW.submit_bound) THEN RAISE EXCEPTION 'immutable alipay bank recipient binding'; END IF; RETURN NEW; END $$;
CREATE TRIGGER alipay_bank_binding_immutable BEFORE UPDATE ON alipay_bank_bindings FOR EACH ROW EXECUTE FUNCTION alipay_bank_binding_guard();
CREATE TRIGGER alipay_bank_binding_no_delete BEFORE DELETE ON alipay_bank_bindings FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE OR REPLACE FUNCTION alipay_bank_binding_insert_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NOT EXISTS(SELECT 1 FROM bank_transfer_lifecycle b JOIN provider_transfer_policies p ON p.transfer_id=b.transfer_id WHERE b.transfer_id=NEW.transfer_id AND b.provider_kind='alipay_bankcard' AND p.request_hash=NEW.request_digest) THEN RAISE EXCEPTION 'alipay bank binding does not match original intent'; END IF; RETURN NEW; END $$;
CREATE TRIGGER alipay_bank_binding_insert BEFORE INSERT ON alipay_bank_bindings FOR EACH ROW EXECUTE FUNCTION alipay_bank_binding_insert_guard();
CREATE TABLE alipay_bank_observations (
 id bigserial PRIMARY KEY,transfer_id bigint NOT NULL REFERENCES alipay_bank_bindings(transfer_id),
 provider_state text NOT NULL,provider_transfer_no text NOT NULL,amount bigint CHECK(amount>0),
 estimated_fee_amount bigint CHECK(estimated_fee_amount>=0),result_state text NOT NULL,reason text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alipay_bank_observation_lookup ON alipay_bank_observations(transfer_id,id);
CREATE TRIGGER alipay_bank_observation_immutable BEFORE UPDATE OR DELETE ON alipay_bank_observations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
