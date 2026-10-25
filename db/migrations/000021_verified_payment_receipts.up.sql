CREATE TABLE verified_payment_receipts (
 order_id bigint PRIMARY KEY REFERENCES orders(id), channel_id bigint NOT NULL CHECK(channel_id>0),
 provider_trade_no text NOT NULL CHECK(octet_length(provider_trade_no) BETWEEN 1 AND 256),
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(channel_id,provider_trade_no)
);
CREATE TRIGGER verified_payment_receipts_immutable BEFORE UPDATE OR DELETE ON verified_payment_receipts FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE order_reporting ADD COLUMN client_ip text NOT NULL DEFAULT '' CHECK(octet_length(client_ip)<=45);
CREATE OR REPLACE FUNCTION guard_order_client_ip() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.client_ip<>'' AND NEW.client_ip IS DISTINCT FROM OLD.client_ip THEN RAISE EXCEPTION 'immutable original payment client IP'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_client_ip_immutable BEFORE UPDATE ON order_reporting FOR EACH ROW EXECUTE FUNCTION guard_order_client_ip();
