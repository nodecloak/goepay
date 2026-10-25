CREATE TABLE settlement_request_terms (
 request_id bigint PRIMARY KEY REFERENCES ledger_requests(id),
 gross_amount bigint NOT NULL CHECK(gross_amount>0),
 fee_amount bigint NOT NULL CHECK(fee_amount>=0),
 net_amount bigint NOT NULL CHECK(net_amount>0),
 fee_rate_bps bigint NOT NULL CHECK(fee_rate_bps BETWEEN 0 AND 10000),
 fee_minimum_cents bigint NOT NULL CHECK(fee_minimum_cents BETWEEN 0 AND 100000000000),
 fee_maximum_cents bigint NOT NULL CHECK(fee_maximum_cents BETWEEN 0 AND 100000000000),
 retained_cents bigint NOT NULL CHECK(retained_cents BETWEEN 0 AND 100000000000),
 daily_limit bigint NOT NULL CHECK(daily_limit BETWEEN 0 AND 1000000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(fee_amount<=gross_amount AND net_amount=gross_amount-fee_amount)
);
CREATE FUNCTION settlement_request_terms_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM ledger_requests WHERE id=NEW.request_id AND kind='settlements' AND amount=NEW.gross_amount) THEN RAISE EXCEPTION 'settlement terms require original gross reservation'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_request_terms_binding BEFORE INSERT ON settlement_request_terms FOR EACH ROW EXECUTE FUNCTION settlement_request_terms_guard();
CREATE TRIGGER settlement_request_terms_immutable BEFORE UPDATE OR DELETE ON settlement_request_terms FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX settlement_request_daily ON ledger_requests(merchant_id,created_at) WHERE kind='settlements';
