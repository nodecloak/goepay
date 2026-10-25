-- A legacy send-state cache is not an authenticated refund result. A terminal
-- cache may be repaired only by new, immutable result evidence born while the
-- original refund was still nonterminal, in the same transaction as its ledger.
ALTER TABLE refunds ADD COLUMN result_evidence_version smallint NOT NULL DEFAULT 1 CHECK(result_evidence_version IN (1,2));
ALTER TABLE refunds ALTER COLUMN result_evidence_version SET DEFAULT 2;
CREATE OR REPLACE FUNCTION refund_allocation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.merchant_amount IS NULL THEN NEW.merchant_amount:=NEW.amount; END IF;
  IF NEW.result_evidence_version<>2 THEN RAISE EXCEPTION 'new refund requires current result evidence'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.amount,NEW.merchant_amount,NEW.fee_amount,NEW.order_id,NEW.merchant_id,NEW.refund_no,NEW.result_evidence_version) IS DISTINCT FROM (OLD.amount,OLD.merchant_amount,OLD.fee_amount,OLD.order_id,OLD.merchant_id,OLD.refund_no,OLD.result_evidence_version) THEN RAISE EXCEPTION 'immutable refund allocation'; END IF;
 RETURN NEW;
END $$;
CREATE TABLE provider_refund_result_evidence (
 refund_id bigint PRIMARY KEY REFERENCES refunds(id),
 operation_id bigint UNIQUE REFERENCES provider_operations(id),
 order_id bigint NOT NULL REFERENCES orders(id),
 channel_id bigint CHECK(channel_id>0),
 amount bigint NOT NULL CHECK(amount>0),
 refund_no text NOT NULL,
 prior_refund_state text NOT NULL CHECK(prior_refund_state IN ('pending','unknown')),
 prior_operation_state text CHECK(prior_operation_state IN ('processing','pending','unknown','succeeded','failed')),
 source text NOT NULL CHECK(source IN ('provider','internal')),
 state text NOT NULL CHECK(state IN ('succeeded','failed')),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 CHECK((source='provider' AND operation_id IS NOT NULL AND channel_id IS NOT NULL AND prior_operation_state IS NOT NULL) OR (source='internal' AND operation_id IS NULL AND channel_id IS NULL AND prior_operation_state IS NULL))
);
CREATE TRIGGER provider_refund_result_immutable BEFORE UPDATE OR DELETE ON provider_refund_result_evidence FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION provider_refund_result_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.created_at<>transaction_timestamp() OR NOT EXISTS(
  SELECT 1 FROM refunds r
  WHERE r.id=NEW.refund_id
  AND (r.order_id,r.amount,r.refund_no,r.status)=(NEW.order_id,NEW.amount,NEW.refund_no,NEW.state)
  AND (NEW.source='internal' OR EXISTS(SELECT 1 FROM provider_operations p WHERE p.refund_id=r.id AND p.id=NEW.operation_id AND p.kind='refund' AND (p.order_id,p.channel_id,p.state)=(NEW.order_id,NEW.channel_id,NEW.prior_operation_state)))
  AND EXISTS(SELECT 1 FROM operation_audit a WHERE a.operation_kind='refund' AND a.operation_id=r.id AND a.from_status=NEW.prior_refund_state AND a.to_status=NEW.state AND a.created_at=transaction_timestamp())
 ) THEN RAISE EXCEPTION 'refund result evidence lacks original nonterminal intent'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_refund_result_birth BEFORE INSERT ON provider_refund_result_evidence FOR EACH ROW EXECUTE FUNCTION provider_refund_result_birth_guard();

CREATE VIEW provider_refund_result_invariants AS
SELECT e.refund_id,
 (r.order_id,r.amount,r.refund_no,r.status)=(e.order_id,e.amount,e.refund_no,e.state)
 AND (e.source='internal' OR (p.kind,p.order_id,p.refund_id,p.channel_id,p.state)=('refund',e.order_id,e.refund_id,e.channel_id,e.state))
 AND e.prior_refund_state IN ('pending','unknown')
 AND EXISTS(SELECT 1 FROM operation_audit a WHERE a.operation_kind='refund' AND a.operation_id=r.id AND a.to_status=e.state)
 AND CASE WHEN r.merchant_amount>0 THEN EXISTS(
  SELECT 1 FROM journals j WHERE j.event_key='refund:'||r.id::text||':resolve'
  AND (SELECT count(*)=2 FROM ledger_entries x WHERE x.journal_id=j.id)
  AND (SELECT COALESCE(sum(x.amount),0)=-r.merchant_amount FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=r.merchant_id AND a.kind='reserved' AND a.scope_id=0)
  AND (SELECT COALESCE(sum(x.amount),0)=r.merchant_amount FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.scope_id=0 AND ((e.state='succeeded' AND a.owner_id=0 AND a.kind='clearing') OR (e.state='failed' AND a.owner_id=r.merchant_id AND a.kind='available')))
 ) ELSE TRUE END
 AND CASE WHEN r.fee_amount>0 THEN EXISTS(
  SELECT 1 FROM journals j WHERE j.event_key='refund-fee:'||r.id::text||':resolve'
  AND (SELECT count(*)=2 FROM ledger_entries x WHERE x.journal_id=j.id)
  AND (SELECT COALESCE(sum(x.amount),0)=-r.fee_amount FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=0 AND a.kind='fee_reserved' AND a.scope_id=0)
  AND (SELECT COALESCE(sum(x.amount),0)=r.fee_amount FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=0 AND a.scope_id=0 AND a.kind=CASE WHEN e.state='succeeded' THEN 'clearing' ELSE 'fees' END)
 ) ELSE TRUE END AS valid
FROM provider_refund_result_evidence e JOIN refunds r ON r.id=e.refund_id LEFT JOIN provider_operations p ON p.id=e.operation_id;

CREATE FUNCTION provider_refund_result_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM provider_refund_result_invariants WHERE refund_id=NEW.refund_id AND valid IS TRUE) THEN RAISE EXCEPTION 'invalid terminal refund evidence'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER provider_refund_result_requires_ledger AFTER INSERT ON provider_refund_result_evidence DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION provider_refund_result_guard();
CREATE FUNCTION refund_required_result_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.result_evidence_version=2 AND NEW.status IN ('succeeded','failed') AND NOT EXISTS(SELECT 1 FROM provider_refund_result_invariants v WHERE v.refund_id=NEW.id AND v.valid IS TRUE) THEN RAISE EXCEPTION 'missing new refund terminal evidence'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER refund_requires_terminal_result AFTER INSERT OR UPDATE ON refunds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION refund_required_result_guard();

CREATE OR REPLACE FUNCTION guard_provider_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind<>OLD.kind OR NEW.business_key<>OLD.business_key OR NEW.order_id<>OLD.order_id OR NEW.channel_id<>OLD.channel_id OR NEW.refund_id IS DISTINCT FROM OLD.refund_id THEN RAISE EXCEPTION 'immutable provider operation identity'; END IF;
 IF OLD.state IN ('succeeded','failed') AND NEW.state<>OLD.state THEN
  IF OLD.kind<>'refund' OR NEW.state NOT IN ('succeeded','failed') OR NOT EXISTS(
   SELECT 1 FROM provider_refund_result_evidence e JOIN refunds r ON r.id=e.refund_id
   WHERE e.operation_id=OLD.id AND e.refund_id=OLD.refund_id AND e.order_id=OLD.order_id AND e.channel_id=OLD.channel_id
   AND e.prior_operation_state=OLD.state AND e.state=NEW.state AND e.created_at=transaction_timestamp()
   AND r.status=e.state
  ) THEN RAISE EXCEPTION 'terminal provider operation'; END IF;
 END IF;
 RETURN NEW;
END $$;
