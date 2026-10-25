CREATE TABLE transfer_batches (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),
 external_no text NOT NULL CHECK(length(external_no) BETWEEN 1 AND 128),
 intent_hash bytea NOT NULL CHECK(octet_length(intent_hash)=32),
 item_count integer NOT NULL CHECK(item_count BETWEEN 1 AND 100),
 amount bigint NOT NULL CHECK(amount>0),cost_amount bigint NOT NULL CHECK(cost_amount>=amount),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),UNIQUE(merchant_id,external_no)
);
CREATE TABLE transfer_batch_items (
 batch_id bigint NOT NULL REFERENCES transfer_batches(id),position integer NOT NULL CHECK(position BETWEEN 1 AND 100),
 transfer_id bigint NOT NULL UNIQUE REFERENCES provider_transfer_attempts(transfer_id),
 provider_kind text NOT NULL CHECK(provider_kind IN ('wxpay','alipay')),
 app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),bank_product text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'unsent' CHECK(state IN ('unsent','claimed','cancelled')),
 claimed_at timestamptz,cancelled_at timestamptz,
 PRIMARY KEY(batch_id,position),UNIQUE(batch_id,transfer_id),
 CHECK((state='unsent' AND claimed_at IS NULL AND cancelled_at IS NULL) OR (state='claimed' AND claimed_at IS NOT NULL AND cancelled_at IS NULL) OR (state='cancelled' AND claimed_at IS NULL AND cancelled_at IS NOT NULL))
);
CREATE TABLE transfer_batch_events (
 id bigserial PRIMARY KEY,batch_id bigint NOT NULL REFERENCES transfer_batches(id),transfer_id bigint REFERENCES transfer_batch_items(transfer_id),
 action text NOT NULL CHECK(action IN ('prepare','claim','result','cancel','query')),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,
 result_state text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 FOREIGN KEY(batch_id,transfer_id) REFERENCES transfer_batch_items(batch_id,transfer_id)
);
CREATE TABLE transfer_unsent_cancellations (
 transfer_id bigint PRIMARY KEY REFERENCES transfer_batch_items(transfer_id),
 cost_amount bigint NOT NULL CHECK(cost_amount>0),created_at timestamptz NOT NULL DEFAULT statement_timestamp()
);
CREATE TRIGGER transfer_batches_immutable BEFORE UPDATE OR DELETE ON transfer_batches FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER transfer_batch_events_immutable BEFORE UPDATE OR DELETE ON transfer_batch_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER transfer_unsent_cancel_immutable BEFORE UPDATE OR DELETE ON transfer_unsent_cancellations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION transfer_unsent_cancel_proof_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM transfer_batch_items i JOIN transfers t ON t.id=i.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id WHERE i.transfer_id=NEW.transfer_id AND i.state='cancelled' AND i.claimed_at IS NULL AND t.status='pending' AND t.cost_amount=NEW.cost_amount AND a.state='processing' AND a.provider_transfer_no='') THEN RAISE EXCEPTION 'unsent cancellation proof does not match original send barrier'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER transfer_unsent_cancel_proof BEFORE INSERT ON transfer_unsent_cancellations FOR EACH ROW EXECUTE FUNCTION transfer_unsent_cancel_proof_guard();
CREATE TRIGGER transfer_batch_items_no_delete BEFORE DELETE ON transfer_batch_items FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION transfer_batch_item_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.batch_id,NEW.position,NEW.transfer_id,NEW.provider_kind,NEW.app_id,NEW.bank_product) IS DISTINCT FROM (OLD.batch_id,OLD.position,OLD.transfer_id,OLD.provider_kind,OLD.app_id,OLD.bank_product) OR OLD.state<>'unsent' AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'immutable batch transfer identity/send barrier'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER transfer_batch_item_guard BEFORE UPDATE ON transfer_batch_items FOR EACH ROW EXECUTE FUNCTION transfer_batch_item_guard();
ALTER TABLE bank_transfer_lifecycle DROP CONSTRAINT bank_transfer_lifecycle_state_check;
ALTER TABLE bank_transfer_lifecycle ADD CONSTRAINT bank_transfer_lifecycle_state_check CHECK(state IN ('processing','succeeded','reversal_pending','reversed','failed','cancelled'));
CREATE FUNCTION bank_unsent_cancel_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='cancelled' AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'immutable platform unsent cancellation'; END IF;
 IF NEW.state='cancelled' AND NOT EXISTS(SELECT 1 FROM transfer_unsent_cancellations c JOIN transfer_batch_items i ON i.transfer_id=c.transfer_id WHERE c.transfer_id=NEW.transfer_id AND i.state='cancelled') THEN RAISE EXCEPTION 'bank cancellation requires durable unsent proof'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bank_unsent_cancel_guard BEFORE UPDATE ON bank_transfer_lifecycle FOR EACH ROW EXECUTE FUNCTION bank_unsent_cancel_guard();
