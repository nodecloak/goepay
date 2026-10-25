CREATE TABLE transfer_batch_executions (
 execution_no text PRIMARY KEY CHECK(execution_no ~ '^[0-9a-f]{64}$'),
 batch_id bigint NOT NULL REFERENCES transfer_batches(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_name text NOT NULL CHECK(length(actor_name)>0 AND length(actor_name)<=128),
 transfer_ids bigint[] NOT NULL CHECK(cardinality(transfer_ids) BETWEEN 1 AND 100),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER batch_execution_immutable BEFORE UPDATE OR DELETE ON transfer_batch_executions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION batch_execution_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM transfer_batches b WHERE b.id=NEW.batch_id AND b.merchant_id=NEW.merchant_id)
 OR (SELECT count(DISTINCT v) FROM unnest(NEW.transfer_ids) v)<>cardinality(NEW.transfer_ids)
 OR (SELECT count(*) FROM transfer_batch_items i WHERE i.batch_id=NEW.batch_id AND i.transfer_id=ANY(NEW.transfer_ids) AND i.state='unsent')<>cardinality(NEW.transfer_ids)
 THEN RAISE EXCEPTION 'invalid authorized batch subset'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER batch_execution_binding BEFORE INSERT ON transfer_batch_executions FOR EACH ROW EXECUTE FUNCTION batch_execution_binding_guard();

CREATE TABLE transfer_batch_execution_claims (
 transfer_id bigint PRIMARY KEY REFERENCES transfer_batch_items(transfer_id),
 execution_no text NOT NULL REFERENCES transfer_batch_executions(execution_no),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER batch_execution_claim_immutable BEFORE UPDATE OR DELETE ON transfer_batch_execution_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION batch_execution_claim_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM transfer_batch_executions e JOIN transfer_batch_items i ON i.batch_id=e.batch_id
  WHERE e.execution_no=NEW.execution_no AND i.transfer_id=NEW.transfer_id AND i.transfer_id=ANY(e.transfer_ids) AND i.state='unsent')
 THEN RAISE EXCEPTION 'claim outside authorized batch subset'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER batch_execution_claim_binding BEFORE INSERT ON transfer_batch_execution_claims FOR EACH ROW EXECUTE FUNCTION batch_execution_claim_guard();
CREATE FUNCTION batch_execution_claim_committed_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM transfer_batch_items WHERE transfer_id=NEW.transfer_id AND state='claimed')
 THEN RAISE EXCEPTION 'authorization claim without committed send barrier'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER batch_execution_claim_committed AFTER INSERT ON transfer_batch_execution_claims DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION batch_execution_claim_committed_guard();
