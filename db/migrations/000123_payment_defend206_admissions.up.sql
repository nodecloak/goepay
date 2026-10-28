-- Evidence that a merchant order number was admitted by create-order human
-- verification (or was not required to verify). An OAuth / mini-program
-- continuation resumes only an order with valid evidence, so an authorization
-- code already exchanged never needs a second verification or exchange.
CREATE TABLE payment_defend206_admissions (
 merchant_id bigint NOT NULL CHECK(merchant_id>0),
 out_trade_no text NOT NULL CHECK(octet_length(out_trade_no) BETWEEN 1 AND 128),
 kind text NOT NULL CHECK(kind IN ('none','0','1','2')),
 policy_fingerprint text NOT NULL CHECK(policy_fingerprint~'^[0-9a-f]{64}$'),
 admitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(merchant_id,out_trade_no)
);
CREATE FUNCTION payment_defend206_admissions_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'payment defend206 admission evidence is immutable';
END $$;
CREATE TRIGGER payment_defend206_admissions_original BEFORE UPDATE OR DELETE ON payment_defend206_admissions FOR EACH ROW EXECUTE FUNCTION payment_defend206_admissions_original();
