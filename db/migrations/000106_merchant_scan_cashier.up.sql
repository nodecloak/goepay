-- Independent merchant-console scan admission. Existing order/fee/protocol
-- columns and historical proof bytes are deliberately unchanged.
CREATE TABLE merchant_scan_registry106 (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 policy_managed boolean NOT NULL DEFAULT false
);
INSERT INTO merchant_scan_registry106(singleton) VALUES(true);
CREATE FUNCTION guard_merchant_scan_registry106() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.policy_managed AND NOT NEW.policy_managed THEN RAISE EXCEPTION 'merchant scan management cannot be forgotten'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_scan_registry106_update BEFORE UPDATE ON merchant_scan_registry106 FOR EACH ROW EXECUTE FUNCTION guard_merchant_scan_registry106();
CREATE TRIGGER merchant_scan_registry106_delete BEFORE DELETE ON merchant_scan_registry106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE merchant_scan_policies106 (
 revision bigint PRIMARY KEY CHECK(revision>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(revision,proof_hash)
);
CREATE TRIGGER merchant_scan_policy106_immutable BEFORE UPDATE OR DELETE ON merchant_scan_policies106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_scan_policy_witnesses106 (
 revision bigint PRIMARY KEY,
 policy_proof bytea NOT NULL CHECK(octet_length(policy_proof)=32),
 witness_proof bytea NOT NULL CHECK(octet_length(witness_proof)=32),
 FOREIGN KEY(revision,policy_proof) REFERENCES merchant_scan_policies106(revision,proof_hash)
);
CREATE TRIGGER merchant_scan_policy_witness106_immutable BEFORE UPDATE OR DELETE ON merchant_scan_policy_witnesses106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_scan_policy_head106 (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL UNIQUE REFERENCES merchant_scan_policy_witnesses106(revision)
);
CREATE TRIGGER merchant_scan_policy_head106_delete BEFORE DELETE ON merchant_scan_policy_head106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE merchant_scan_intents106 (
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 request_id uuid NOT NULL,
 out_trade_no text NOT NULL UNIQUE CHECK(length(out_trade_no) BETWEEN 1 AND 128),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(merchant_id,request_id)
);
CREATE TRIGGER merchant_scan_intent106_immutable BEFORE UPDATE OR DELETE ON merchant_scan_intents106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_scan_order_sources106 (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 merchant_id bigint NOT NULL,
 request_id uuid NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(merchant_id,request_id),
 FOREIGN KEY(merchant_id,request_id) REFERENCES merchant_scan_intents106(merchant_id,request_id)
);
CREATE TRIGGER merchant_scan_order_source106_immutable BEFORE UPDATE OR DELETE ON merchant_scan_order_sources106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Also closes declarations interrupted before an order was allocated. A
-- missing order must never require the browser to invent a replacement charge.
CREATE TABLE merchant_scan_closures106 (
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 request_id uuid NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(merchant_id,request_id)
);
CREATE TRIGGER merchant_scan_closure106_immutable BEFORE UPDATE OR DELETE ON merchant_scan_closures106 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
