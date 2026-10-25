CREATE TABLE provider_operations (
 id bigserial PRIMARY KEY,kind text NOT NULL CHECK(kind IN ('close','refund')),
 business_key text NOT NULL,order_id bigint NOT NULL REFERENCES orders(id),
 refund_id bigint REFERENCES refunds(id),channel_id bigint NOT NULL,
 state text NOT NULL DEFAULT 'processing' CHECK(state IN ('processing','pending','unknown','succeeded','failed')),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(kind,business_key)
);
CREATE TABLE provider_reconciliations (
 id bigserial PRIMARY KEY,order_id bigint NOT NULL REFERENCES orders(id),
 previous_status text NOT NULL,event_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(order_id,event_id)
);
CREATE TRIGGER provider_reconciliation_immutable BEFORE UPDATE OR DELETE ON provider_reconciliations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE OR REPLACE FUNCTION guard_provider_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind<>OLD.kind OR NEW.business_key<>OLD.business_key OR NEW.order_id<>OLD.order_id OR NEW.channel_id<>OLD.channel_id OR NEW.refund_id IS DISTINCT FROM OLD.refund_id THEN RAISE EXCEPTION 'immutable provider operation identity'; END IF;
 IF OLD.state IN ('succeeded','failed') AND NEW.state<>OLD.state THEN RAISE EXCEPTION 'terminal provider operation'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_operation_guard BEFORE UPDATE ON provider_operations FOR EACH ROW EXECUTE FUNCTION guard_provider_operation();
CREATE TRIGGER provider_operation_no_delete BEFORE DELETE ON provider_operations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
