-- The existing parent amount stays the original frozen amount. An effective
-- remaining amount is always read through the independently authenticated head.
ALTER TABLE payment_authorization_releases ADD COLUMN remaining_source_id bigint;
ALTER TABLE payment_authorization_releases ADD COLUMN remaining_source_revision bigint NOT NULL DEFAULT 0 CHECK(remaining_source_revision BETWEEN 0 AND 100);
ALTER TABLE payment_authorization_releases ADD COLUMN remaining_source_hash bytea;
ALTER TABLE payment_authorization_releases ADD CONSTRAINT authorization_remaining_head_shape CHECK
 ((remaining_source_revision=0 AND remaining_source_id IS NULL AND remaining_source_hash IS NULL) OR
  (remaining_source_revision>0 AND remaining_source_id IS NOT NULL AND remaining_source_hash IS NOT NULL AND octet_length(remaining_source_hash)=32));

CREATE TABLE authorization_remaining_sources (
 id bigserial PRIMARY KEY,order_id bigint NOT NULL REFERENCES payment_authorization_releases(order_id),
 revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 100),previous_id bigint REFERENCES authorization_remaining_sources(id),previous_hash bytea,
 observation_id text NOT NULL REFERENCES preauth_observations(id),terms jsonb NOT NULL CHECK(jsonb_typeof(terms)='object'),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(order_id,revision),UNIQUE(id,order_id,revision),
 CHECK((revision=1 AND previous_id IS NULL AND previous_hash IS NULL) OR (revision>1 AND previous_id IS NOT NULL AND previous_hash IS NOT NULL AND octet_length(previous_hash)=32))
);
CREATE TRIGGER authorization_remaining_source_immutable BEFORE UPDATE OR DELETE ON authorization_remaining_sources FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE payment_authorization_releases ADD CONSTRAINT authorization_remaining_head_source FOREIGN KEY(remaining_source_id,order_id,remaining_source_revision) REFERENCES authorization_remaining_sources(id,order_id,revision) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE authorization_remaining_claims (
 order_id bigint PRIMARY KEY REFERENCES payment_authorization_releases(order_id),source_id bigint NOT NULL UNIQUE,source_revision bigint NOT NULL,
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(source_id,order_id,source_revision) REFERENCES authorization_remaining_sources(id,order_id,revision)
);
CREATE TRIGGER authorization_remaining_claim_immutable BEFORE UPDATE OR DELETE ON authorization_remaining_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX authorization_remaining_audit_order ON backoffice_audit((changes->>'order_id'),resource_id,operation) WHERE resource='authorization-remaining-sources';

CREATE OR REPLACE FUNCTION guard_authorization_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.order_id,NEW.channel_id,NEW.request_no,NEW.auth_no,NEW.amount) IS DISTINCT FROM (OLD.order_id,OLD.channel_id,OLD.request_no,OLD.auth_no,OLD.amount) THEN RAISE EXCEPTION 'authorization release identity is immutable';END IF;
 IF OLD.sent AND NOT NEW.sent OR OLD.state='succeeded' AND NEW.state<>'succeeded' OR OLD.sent AND NEW.state='checking' THEN RAISE EXCEPTION 'authorization release cannot be resent or reversed';END IF;
 IF (NEW.remaining_source_id,NEW.remaining_source_revision,NEW.remaining_source_hash) IS DISTINCT FROM (OLD.remaining_source_id,OLD.remaining_source_revision,OLD.remaining_source_hash) THEN
  IF OLD.sent OR NEW.sent OR OLD.state NOT IN('checking','reversal_pending') OR NEW.state<>'checking'
   OR NEW.remaining_source_revision<>OLD.remaining_source_revision+1 OR NEW.remaining_source_id IS NULL OR NEW.remaining_source_hash IS NULL
   OR NOT EXISTS(SELECT 1 FROM authorization_remaining_sources s WHERE (s.id,s.order_id,s.revision)=(NEW.remaining_source_id,NEW.order_id,NEW.remaining_source_revision)
    AND s.previous_id IS NOT DISTINCT FROM OLD.remaining_source_id AND s.previous_hash IS NOT DISTINCT FROM OLD.remaining_source_hash AND s.proof_hash=NEW.remaining_source_hash AND s.transaction_id=pg_current_xact_id())
  THEN RAISE EXCEPTION 'remaining release requires a new exact unsent source transaction';END IF;
 END IF;
 IF NEW.sent AND NOT OLD.sent AND NEW.remaining_source_revision>0 AND NOT EXISTS(SELECT 1 FROM authorization_remaining_claims c WHERE (c.order_id,c.source_id,c.source_revision)=(NEW.order_id,NEW.remaining_source_id,NEW.remaining_source_revision) AND c.transaction_id=pg_current_xact_id()) THEN RAISE EXCEPTION 'remaining release requires its original atomic claim';END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION authorization_remaining_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oid bigint;
BEGIN
 oid:=NEW.order_id;
 IF NOT EXISTS(SELECT 1 FROM payment_authorization_releases r JOIN payment_authorizations a ON a.order_id=r.order_id WHERE r.order_id=oid
  AND (r.channel_id,r.auth_no,r.amount)=(a.channel_id,a.auth_no,a.amount) AND r.request_no='UA'||r.order_id::text
  AND ((r.remaining_source_revision=0 AND NOT EXISTS(SELECT 1 FROM authorization_remaining_sources s WHERE s.order_id=r.order_id) AND NOT EXISTS(SELECT 1 FROM authorization_remaining_claims c WHERE c.order_id=r.order_id))
   OR (r.remaining_source_revision>0 AND (SELECT count(*) FROM authorization_remaining_sources s WHERE s.order_id=r.order_id)=r.remaining_source_revision
    AND EXISTS(SELECT 1 FROM authorization_remaining_sources s WHERE (s.id,s.order_id,s.revision,s.proof_hash)=(r.remaining_source_id,r.order_id,r.remaining_source_revision,r.remaining_source_hash))
    AND ((NOT r.sent AND NOT EXISTS(SELECT 1 FROM authorization_remaining_claims c WHERE c.order_id=r.order_id)) OR
     (r.sent AND EXISTS(SELECT 1 FROM authorization_remaining_claims c WHERE (c.order_id,c.source_id,c.source_revision)=(r.order_id,r.remaining_source_id,r.remaining_source_revision)))))))
 THEN RAISE EXCEPTION 'incomplete original authorization remaining source';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER authorization_remaining_parent_complete AFTER INSERT OR UPDATE ON payment_authorization_releases DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION authorization_remaining_complete();
CREATE CONSTRAINT TRIGGER authorization_remaining_source_complete AFTER INSERT ON authorization_remaining_sources DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION authorization_remaining_complete();
CREATE CONSTRAINT TRIGGER authorization_remaining_claim_complete AFTER INSERT ON authorization_remaining_claims DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION authorization_remaining_complete();
