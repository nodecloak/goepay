-- Runtime IDs are explicitly bound to the loaded provider registry by the
-- private authenticated identity, never to editable directory names or IDs.
ALTER TABLE orders ADD COLUMN runtime_fee_quote_required boolean NOT NULL DEFAULT false;
CREATE FUNCTION guard_runtime_fee_quote_marker() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.runtime_fee_quote_required IS DISTINCT FROM OLD.runtime_fee_quote_required THEN
  RAISE EXCEPTION 'runtime fee birth marker is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_runtime_fee_quote_marker_immutable BEFORE UPDATE OF runtime_fee_quote_required ON orders FOR EACH ROW EXECUTE FUNCTION guard_runtime_fee_quote_marker();
CREATE TABLE runtime_channel_policies (
 channel_id bigint PRIMARY KEY CHECK(channel_id>0),
 pay_type text NOT NULL CHECK(pay_type IN ('alipay','wxpay')),
 identity_hash bytea NOT NULL CHECK(octet_length(identity_hash)=32),
 retained_bps bigint NOT NULL CHECK(retained_bps BETWEEN 0 AND 10000),
 mode integer NOT NULL CHECK(mode=0),
 enabled boolean NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER runtime_channel_policy_no_delete BEFORE DELETE ON runtime_channel_policies FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE provider_route_fee_quotes (
 merchant_id bigint NOT NULL,
 out_trade_no text NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(merchant_id,out_trade_no),
 FOREIGN KEY(merchant_id,out_trade_no) REFERENCES provider_route_assignments(merchant_id,out_trade_no)
);
CREATE TRIGGER provider_route_fee_quote_immutable BEFORE UPDATE OR DELETE ON provider_route_fee_quotes FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE order_route_fee_terms (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 merchant_id bigint NOT NULL,
 out_trade_no text NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(merchant_id,out_trade_no) REFERENCES provider_route_fee_quotes(merchant_id,out_trade_no),
 UNIQUE(merchant_id,out_trade_no)
);
CREATE TRIGGER order_route_fee_terms_immutable BEFORE UPDATE OR DELETE ON order_route_fee_terms FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
