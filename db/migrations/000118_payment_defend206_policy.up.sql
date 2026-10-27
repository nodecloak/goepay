-- A new independent policy. Descriptive legacy settings and risk102 proofs are
-- deliberately neither copied nor activated. No configuration row is seeded.
CREATE TABLE payment_defend206_policy_commits (
 revision bigint PRIMARY KEY CHECK(revision>0),
 commitment text NOT NULL CHECK(commitment ~ '^[a-f0-9]{64}$'),
 previous_commitment text NOT NULL CHECK(previous_commitment='' OR previous_commitment ~ '^[a-f0-9]{64}$'),
 audit_id bigint NOT NULL UNIQUE REFERENCES backoffice_audit(id),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 birth_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER payment_defend206_commits_immutable BEFORE UPDATE OR DELETE ON payment_defend206_policy_commits FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER payment_defend206_commits_no_truncate BEFORE TRUNCATE ON payment_defend206_policy_commits FOR EACH STATEMENT EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE payment_defend206_policy (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL UNIQUE REFERENCES payment_defend206_policy_commits(revision),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER payment_defend206_policy_no_delete BEFORE DELETE ON payment_defend206_policy FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER payment_defend206_policy_no_truncate BEFORE TRUNCATE ON payment_defend206_policy FOR EACH STATEMENT EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_payment_defend206_policy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.facts->>'Version' IS DISTINCT FROM '1' OR NEW.facts->>'Revision' IS DISTINCT FROM NEW.revision::text
 OR (TG_OP='INSERT' AND NEW.revision<>1)
 OR (TG_OP='UPDATE' AND NEW.revision<>OLD.revision+1)
 OR NOT EXISTS(
  SELECT 1 FROM payment_defend206_policy_commits c JOIN backoffice_audit a ON a.id=c.audit_id
  WHERE c.revision=NEW.revision AND c.commitment=encode(NEW.proof_hash,'hex')
   AND c.birth_xid=pg_current_xact_id()
   AND a.resource='payment-defend206' AND a.resource_id=1 AND a.operation='configure'
   AND a.actor_role='admin' AND a.actor_id=c.actor_id
   AND a.changes->>'revision'=c.revision::text AND a.changes->>'commitment'=c.commitment
 ) THEN RAISE EXCEPTION 'invalid payment defend policy source'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payment_defend206_policy_source BEFORE INSERT OR UPDATE ON payment_defend206_policy FOR EACH ROW EXECUTE FUNCTION guard_payment_defend206_policy();
