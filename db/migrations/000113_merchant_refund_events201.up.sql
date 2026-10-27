-- NULL identifies historical/unknown entry provenance. It is never promoted.
ALTER TABLE refunds ADD COLUMN event_origin201 text CHECK(event_origin201 IN ('api','platform'));
CREATE FUNCTION guard_refund_event_origin201() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.event_origin201 IS DISTINCT FROM OLD.event_origin201 THEN RAISE EXCEPTION 'refund event origin is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER refund_event_origin201_immutable BEFORE UPDATE ON refunds FOR EACH ROW EXECUTE FUNCTION guard_refund_event_origin201();

CREATE TABLE merchant_refund_origins201 (
 refund_id bigint PRIMARY KEY REFERENCES refunds(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
CREATE TRIGGER merchant_refund_origins201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_origins201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION require_refund_event_origin201() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.event_origin201 IS NOT NULL AND NOT EXISTS(SELECT 1 FROM merchant_refund_origins201 WHERE refund_id=NEW.id) THEN RAISE EXCEPTION 'authenticated refund event origin required'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER refund_event_origin201_required AFTER INSERT ON refunds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_refund_event_origin201();

CREATE TABLE merchant_event_subscription_revisions201 (
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 revision bigint NOT NULL CHECK(revision>0),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 PRIMARY KEY(merchant_id,revision), UNIQUE(merchant_id,revision,mac)
);
CREATE TRIGGER merchant_event_subscription_revisions201_immutable BEFORE UPDATE OR DELETE ON merchant_event_subscription_revisions201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_event_subscription_heads201 (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),
 revision bigint NOT NULL,
 mac bytea NOT NULL,
 FOREIGN KEY(merchant_id,revision,mac) REFERENCES merchant_event_subscription_revisions201(merchant_id,revision,mac)
);
CREATE TRIGGER merchant_event_subscription_heads201_immutable_delete BEFORE DELETE ON merchant_event_subscription_heads201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE merchant_refund_events201 (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 refund_id bigint NOT NULL UNIQUE REFERENCES merchant_refund_origins201(refund_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 16384),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
CREATE INDEX merchant_refund_events201_merchant ON merchant_refund_events201(merchant_id,id);
CREATE TRIGGER merchant_refund_events201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_events201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_refund_event_claims201 (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_id bigint NOT NULL REFERENCES merchant_refund_events201(id),
 sequence integer NOT NULL CHECK(sequence>0),
 job_id bigint NOT NULL REFERENCES jobs(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 UNIQUE(event_id,sequence)
);
CREATE TRIGGER merchant_refund_event_claims201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_event_claims201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_refund_event_results201 (
 claim_id bigint PRIMARY KEY REFERENCES merchant_refund_event_claims201(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
CREATE TRIGGER merchant_refund_event_results201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_event_results201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE merchant_refund_event_closed_notices201 (
 event_id bigint PRIMARY KEY REFERENCES merchant_refund_events201(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 8192),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
CREATE TRIGGER merchant_refund_event_closed_notices201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_event_closed_notices201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_refund_event_notice_claims201 (
 event_id bigint PRIMARY KEY REFERENCES merchant_refund_event_closed_notices201(event_id),
 job_id bigint NOT NULL UNIQUE REFERENCES jobs(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
CREATE TRIGGER merchant_refund_event_notice_claims201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_event_notice_claims201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE merchant_refund_event_notice_results201 (
 event_id bigint PRIMARY KEY REFERENCES merchant_refund_event_notice_claims201(event_id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32)
);
CREATE TRIGGER merchant_refund_event_notice_results201_immutable BEFORE UPDATE OR DELETE ON merchant_refund_event_notice_results201 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
