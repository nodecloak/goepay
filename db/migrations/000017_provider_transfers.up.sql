ALTER TABLE transfers ADD COLUMN biz_no text;
UPDATE transfers SET biz_no=id::text;
ALTER TABLE transfers ALTER COLUMN biz_no SET NOT NULL;
CREATE UNIQUE INDEX transfer_biz_no_unique ON transfers(biz_no);
ALTER TABLE transfers ADD COLUMN cost_amount bigint;
ALTER TABLE transfers ADD COLUMN fee_amount bigint NOT NULL DEFAULT 0 CHECK(fee_amount>=0);
ALTER TABLE transfers ADD COLUMN fee_rate_bps integer NOT NULL DEFAULT 0 CHECK(fee_rate_bps BETWEEN 0 AND 10000);
UPDATE transfers SET cost_amount=amount;
ALTER TABLE transfers ALTER COLUMN cost_amount SET NOT NULL;
ALTER TABLE transfers ADD CONSTRAINT transfer_cost_balanced CHECK(cost_amount=amount+fee_amount);
CREATE OR REPLACE FUNCTION transfer_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.biz_no IS NULL THEN NEW.biz_no:=NEW.id::text; END IF;
  IF NEW.cost_amount IS NULL THEN NEW.cost_amount:=NEW.amount; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.merchant_id,NEW.transfer_no,NEW.amount,NEW.cost_amount,NEW.fee_amount,NEW.fee_rate_bps,NEW.biz_no) IS DISTINCT FROM (OLD.merchant_id,OLD.transfer_no,OLD.amount,OLD.cost_amount,OLD.fee_amount,OLD.fee_rate_bps,OLD.biz_no) THEN
 RAISE EXCEPTION 'immutable transfer identity and cost'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER transfer_snapshot_immutable BEFORE INSERT OR UPDATE ON transfers FOR EACH ROW EXECUTE FUNCTION transfer_snapshot_guard();
CREATE TABLE provider_transfer_attempts (
 transfer_id bigint PRIMARY KEY REFERENCES transfers(id), channel_id bigint NOT NULL CHECK(channel_id>0), recipient_cipher bytea NOT NULL,
 state text NOT NULL DEFAULT 'processing' CHECK(state IN ('processing','pending','unknown','succeeded','failed')),
 provider_transfer_no text NOT NULL DEFAULT '',pay_date text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION transfer_attempt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.transfer_id,NEW.channel_id,NEW.recipient_cipher) IS DISTINCT FROM (OLD.transfer_id,OLD.channel_id,OLD.recipient_cipher) THEN RAISE EXCEPTION 'immutable transfer recipient and channel'; END IF;
 IF OLD.state IN ('succeeded','failed') AND (NEW.state,NEW.provider_transfer_no,NEW.pay_date) IS DISTINCT FROM (OLD.state,OLD.provider_transfer_no,OLD.pay_date) THEN RAISE EXCEPTION 'immutable terminal transfer outcome'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_transfer_attempt_immutable BEFORE UPDATE ON provider_transfer_attempts FOR EACH ROW EXECUTE FUNCTION transfer_attempt_guard();
CREATE TRIGGER provider_transfer_attempt_no_delete BEFORE DELETE ON provider_transfer_attempts FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE provider_transfer_proofs (
 transfer_id bigint PRIMARY KEY REFERENCES provider_transfer_attempts(transfer_id),
 state text NOT NULL CHECK(state IN ('applying','pending','unknown','succeeded','failed')),file_id text NOT NULL DEFAULT '',download_cipher bytea,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION transfer_proof_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.transfer_id<>OLD.transfer_id OR (OLD.file_id<>'' AND NEW.file_id<>OLD.file_id) THEN RAISE EXCEPTION 'immutable transfer proof identity'; END IF;
 IF OLD.state IN ('succeeded','failed') AND (NEW.state,NEW.file_id,NEW.download_cipher) IS DISTINCT FROM (OLD.state,OLD.file_id,OLD.download_cipher) THEN RAISE EXCEPTION 'immutable terminal transfer proof'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_transfer_proof_immutable BEFORE UPDATE ON provider_transfer_proofs FOR EACH ROW EXECUTE FUNCTION transfer_proof_guard();
CREATE TRIGGER provider_transfer_proof_no_delete BEFORE DELETE ON provider_transfer_proofs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
