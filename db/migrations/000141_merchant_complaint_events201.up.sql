-- Complaint event subscription delivery (Pro 订阅事件 交易投诉). The merchant
-- event-subscription webhook (migration 000113, package internal/refundevent201)
-- so far carried only refund events; this adds the complaint event alongside it,
-- reusing the SAME per-merchant subscription (merchant_event_subscription_*201),
-- the same RSA signing, the same consecutive-failure / auto-disable counter, and
-- the same private-address rejection. ux-complain's provider sync
-- (internal/payment/complaints133_sync.go upsertComplaint133) now enqueues one
-- event per complaint STATE CHANGE for the complaint's owning merchant.
--
-- Idempotency: a complaint event is UNIQUE on (complaint_id, local_state), so a
-- re-sync that upserts the same complaint at the same state inserts nothing and
-- enqueues no duplicate; only a forward state change (open -> processing -> done)
-- creates a new event. Identity is immutable and append-only (same
-- reject_ledger_mutation trigger as the refund event tables).
--
-- Forward-only: applied as-is by db/migrate.go; idempotent and safe on existing
-- installs (IF NOT EXISTS + guarded triggers). No .down.sql. It adds tables only
-- and changes no existing row.

CREATE TABLE IF NOT EXISTS merchant_complaint_events201 (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 complaint_id bigint NOT NULL REFERENCES complaints133(id),
 -- The coarse local state this event reports (open/processing/done). The pair
 -- (complaint_id, state) is the idempotency key: a re-sync at the same state is
 -- a no-op; each distinct state transition is one deliverable event.
 local_state text NOT NULL CHECK(local_state IN ('open','processing','done')),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 16384),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 UNIQUE(complaint_id, local_state)
);
CREATE INDEX IF NOT EXISTS merchant_complaint_events201_merchant ON merchant_complaint_events201(merchant_id,id);
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='merchant_complaint_events201_immutable') THEN
  CREATE TRIGGER merchant_complaint_events201_immutable BEFORE UPDATE OR DELETE ON merchant_complaint_events201
  FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
 END IF;
END $$;

CREATE TABLE IF NOT EXISTS merchant_complaint_event_claims201 (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_id bigint NOT NULL REFERENCES merchant_complaint_events201(id),
 sequence integer NOT NULL CHECK(sequence>0),
 job_id bigint NOT NULL REFERENCES jobs(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 UNIQUE(event_id,sequence)
);
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='merchant_complaint_event_claims201_immutable') THEN
  CREATE TRIGGER merchant_complaint_event_claims201_immutable BEFORE UPDATE OR DELETE ON merchant_complaint_event_claims201
  FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
 END IF;
END $$;

CREATE TABLE IF NOT EXISTS merchant_complaint_event_results201 (
 claim_id bigint PRIMARY KEY REFERENCES merchant_complaint_event_claims201(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='merchant_complaint_event_results201_immutable') THEN
  CREATE TRIGGER merchant_complaint_event_results201_immutable BEFORE UPDATE OR DELETE ON merchant_complaint_event_results201
  FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
 END IF;
END $$;
