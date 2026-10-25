-- Only a keyed, one-way account index is stored outside the encrypted intent.
CREATE TABLE provider_transfer_policies (
 transfer_id bigint PRIMARY KEY REFERENCES provider_transfer_attempts(transfer_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX provider_transfer_recipient_lookup ON provider_transfer_policies(merchant_id,recipient_hash,transfer_id);
CREATE TRIGGER provider_transfer_policies_immutable BEFORE UPDATE OR DELETE ON provider_transfer_policies FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE transfers ADD COLUMN resolved_at timestamptz;
UPDATE transfers SET resolved_at=updated_at WHERE status IN ('succeeded','failed');
CREATE OR REPLACE FUNCTION transfer_resolution_time_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.resolved_at IS NOT NULL AND NEW.resolved_at IS DISTINCT FROM OLD.resolved_at THEN RAISE EXCEPTION 'immutable transfer resolution time'; END IF;
 IF NEW.status IN ('succeeded','failed') AND NEW.resolved_at IS NULL THEN NEW.resolved_at:=now(); END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER transfer_resolution_time_immutable BEFORE INSERT OR UPDATE ON transfers FOR EACH ROW EXECUTE FUNCTION transfer_resolution_time_guard();
