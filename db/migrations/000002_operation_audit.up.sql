CREATE TABLE operation_audit (
 id bigserial PRIMARY KEY, operation_kind text NOT NULL CHECK(operation_kind IN ('refund','transfer')),
 operation_id bigint NOT NULL, merchant_id bigint NOT NULL REFERENCES merchants(id),
 from_status text, to_status text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operation_audit_lookup ON operation_audit(operation_kind,operation_id,id);
CREATE TRIGGER operation_audit_immutable BEFORE UPDATE OR DELETE ON operation_audit FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE OR REPLACE FUNCTION audit_operation_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous text; kind text;
BEGIN
 IF TG_OP='UPDATE' THEN
  previous:=OLD.status;
  IF NEW.status=OLD.status THEN RETURN NEW; END IF;
  IF OLD.status IN ('succeeded','failed') OR NEW.status='pending' THEN RAISE EXCEPTION 'terminal operation cannot transition'; END IF;
 END IF;
 kind:=CASE WHEN TG_TABLE_NAME='refunds' THEN 'refund' ELSE 'transfer' END;
 INSERT INTO operation_audit(operation_kind,operation_id,merchant_id,from_status,to_status) VALUES(kind,NEW.id,NEW.merchant_id,previous,NEW.status);
 RETURN NEW;
END $$;
CREATE TRIGGER refunds_audit AFTER INSERT OR UPDATE OF status ON refunds FOR EACH ROW EXECUTE FUNCTION audit_operation_transition();
CREATE TRIGGER transfers_audit AFTER INSERT OR UPDATE OF status ON transfers FOR EACH ROW EXECUTE FUNCTION audit_operation_transition();
