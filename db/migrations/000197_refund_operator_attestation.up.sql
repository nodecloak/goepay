-- Operator-attested refund results. On channels that cannot query refund
-- outcomes, a refund whose provider answer was lost stays unknown with its
-- funds reserved: no automatic path can reach a final state. A super
-- administrator who verified the outcome with the provider records it here.
-- The attestation is immutable and is the only accepted proof for the
-- 'operator' result evidence source added below.
CREATE TABLE refund_operator_attestations (
 refund_id bigint PRIMARY KEY REFERENCES refunds(id),
 operation_id bigint UNIQUE REFERENCES provider_operations(id),
 state text NOT NULL CHECK(state IN ('succeeded','failed')),
 provider_reference text NOT NULL CHECK(length(provider_reference) BETWEEN 1 AND 128),
 note text NOT NULL CHECK(length(note) BETWEEN 1 AND 500),
 admin_username text NOT NULL,
 admin_id bigint NOT NULL,
 admin_auth_version bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 CHECK(operation_id IS NOT NULL OR state='failed')
);
CREATE TRIGGER refund_operator_attestation_immutable BEFORE UPDATE OR DELETE ON refund_operator_attestations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

ALTER TABLE provider_refund_result_evidence DROP CONSTRAINT provider_refund_result_evidence_source_check;
ALTER TABLE provider_refund_result_evidence ADD CONSTRAINT provider_refund_result_evidence_source_check CHECK(source IN ('provider','internal','operator'));
ALTER TABLE provider_refund_result_evidence DROP CONSTRAINT provider_refund_result_evidence_check;
ALTER TABLE provider_refund_result_evidence ADD CONSTRAINT provider_refund_result_evidence_check CHECK(
 (source IN ('provider','operator') AND operation_id IS NOT NULL AND channel_id IS NOT NULL AND prior_operation_state IS NOT NULL)
 OR (source='internal' AND operation_id IS NULL AND channel_id IS NULL AND prior_operation_state IS NULL)
);

-- Operator evidence is born only together with its attestation, in the same
-- transaction, for the same operation and final state.
CREATE OR REPLACE FUNCTION provider_refund_result_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.created_at<>transaction_timestamp() OR NOT EXISTS(
  SELECT 1 FROM refunds r
  WHERE r.id=NEW.refund_id
  AND (r.order_id,r.amount,r.refund_no,r.status)=(NEW.order_id,NEW.amount,NEW.refund_no,NEW.state)
  AND (NEW.source='internal' OR EXISTS(SELECT 1 FROM provider_operations p WHERE p.refund_id=r.id AND p.id=NEW.operation_id AND p.kind='refund' AND (p.order_id,p.channel_id,p.state)=(NEW.order_id,NEW.channel_id,NEW.prior_operation_state)))
  AND (NEW.source<>'operator' OR EXISTS(SELECT 1 FROM refund_operator_attestations o WHERE o.refund_id=r.id AND o.operation_id=NEW.operation_id AND o.state=NEW.state AND o.created_at=transaction_timestamp()))
  AND EXISTS(SELECT 1 FROM operation_audit a WHERE a.operation_kind='refund' AND a.operation_id=r.id AND a.from_status=NEW.prior_refund_state AND a.to_status=NEW.state AND a.created_at=transaction_timestamp())
 ) THEN RAISE EXCEPTION 'refund result evidence lacks original nonterminal intent'; END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE VIEW provider_refund_result_invariants AS
SELECT e.refund_id,
 (r.order_id,r.amount,r.refund_no,r.status)=(e.order_id,e.amount,e.refund_no,e.state)
 AND (e.source='internal' OR (p.kind,p.order_id,p.refund_id,p.channel_id,p.state)=('refund',e.order_id,e.refund_id,e.channel_id,e.state))
 AND (e.source<>'operator' OR EXISTS(SELECT 1 FROM refund_operator_attestations o WHERE o.refund_id=e.refund_id AND o.operation_id=e.operation_id AND o.state=e.state))
 AND e.prior_refund_state IN ('pending','unknown')
 AND EXISTS(SELECT 1 FROM operation_audit a WHERE a.operation_kind='refund' AND a.operation_id=r.id AND a.to_status=e.state)
 AND refund_merchant_resolution_valid(r.id) IS TRUE
 AND CASE WHEN r.fee_amount>0 THEN EXISTS(
  SELECT 1 FROM journals j WHERE j.event_key='refund-fee:'||r.id::text||':resolve'
  AND (SELECT count(*)=2 FROM ledger_entries x WHERE x.journal_id=j.id)
  AND (SELECT COALESCE(sum(x.amount),0)=-r.fee_amount FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=0 AND a.kind='fee_reserved' AND a.scope_id=0)
  AND (SELECT COALESCE(sum(x.amount),0)=r.fee_amount FROM ledger_entries x JOIN accounts a ON a.id=x.account_id WHERE x.journal_id=j.id AND a.owner_id=0 AND a.scope_id=0 AND a.kind=CASE WHEN e.state='succeeded' THEN 'clearing' ELSE 'fees' END)
 ) ELSE TRUE END AS valid
FROM provider_refund_result_evidence e JOIN refunds r ON r.id=e.refund_id LEFT JOIN provider_operations p ON p.id=e.operation_id;
