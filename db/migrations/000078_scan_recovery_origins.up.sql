-- A new barcode send may opt into bounded recovery only before its first I/O.
-- Existing accounts keep their original MAC and never acquire a recovery source.
ALTER TABLE provider_attempt_accounts
 ADD COLUMN recovery_version smallint NOT NULL DEFAULT 0,
 ADD COLUMN recovery_source_hash bytea,
 ADD CONSTRAINT scan_recovery_account_shape CHECK (
  (recovery_version=0 AND recovery_source_hash IS NULL)
  OR (recovery_version=1 AND recovery_source_hash IS NOT NULL AND octet_length(recovery_source_hash)=32)
 );

CREATE TABLE scan_recovery_origins (
 attempt_id bigint PRIMARY KEY REFERENCES provider_attempts(id),
 order_id bigint NOT NULL UNIQUE REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 trade_no text NOT NULL,
 provider text NOT NULL CHECK(provider IN('alipay','wxpay')),
 amount bigint NOT NULL CHECK(amount>0),
 request_identity_hash bytea NOT NULL CHECK(octet_length(request_identity_hash)=32),
 account_scope_hash bytea NOT NULL CHECK(octet_length(account_scope_hash)=32),
 policy text NOT NULL CHECK(policy='original-barcode-v1'),
 minimum_wait_seconds integer NOT NULL CHECK(minimum_wait_seconds=15),
 query_interval_seconds integer NOT NULL CHECK(query_interval_seconds=3),
 maximum_queries integer NOT NULL CHECK(maximum_queries=6),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at=created_at+interval '120 seconds'),
 transaction_id text NOT NULL CHECK(transaction_id<>''),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scan_recovery_origin_immutable BEFORE UPDATE OR DELETE ON scan_recovery_origins FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION scan_recovery_origin_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM provider_attempts p JOIN orders o ON o.id=p.order_id
  JOIN provider_attempt_identities i ON i.attempt_id=p.id
  JOIN provider_attempt_accounts a ON a.attempt_id=p.id
  WHERE p.id=NEW.attempt_id AND p.method='scan' AND p.channel_id=NEW.channel_id
   AND (o.id,o.merchant_id,o.trade_no,o.type,o.amount)=(NEW.order_id,NEW.merchant_id,NEW.trade_no,NEW.provider,NEW.amount)
   AND i.request_hash=NEW.request_identity_hash AND a.recovery_version=1 AND a.recovery_source_hash=NEW.proof_hash
 ) THEN RAISE EXCEPTION 'original barcode recovery source mismatch';END IF;
 IF (SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-origins' AND resource_id=NEW.attempt_id)<>1
 OR (SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-origins' AND resource_id=NEW.attempt_id
      AND actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='create'
      AND changes=jsonb_build_object('proof',encode(NEW.proof_hash,'hex'),'order_id',NEW.order_id::text))<>1
 THEN RAISE EXCEPTION 'missing original barcode recovery audit';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER scan_recovery_origin_source AFTER INSERT ON scan_recovery_origins DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION scan_recovery_origin_guard();

CREATE TABLE scan_recovery_eligibility (
 attempt_id bigint PRIMARY KEY REFERENCES scan_recovery_origins(attempt_id),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 created_at timestamptz NOT NULL,
 transaction_id text NOT NULL CHECK(transaction_id<>''),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scan_recovery_eligibility_immutable BEFORE UPDATE OR DELETE ON scan_recovery_eligibility FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION scan_recovery_eligibility_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM scan_recovery_origins o WHERE o.attempt_id=NEW.attempt_id AND o.proof_hash=NEW.source_hash AND NEW.created_at>=o.created_at AND NEW.created_at<o.expires_at)
 THEN RAISE EXCEPTION 'original barcode eligibility source mismatch';END IF;
 IF (SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-eligibility' AND resource_id=NEW.attempt_id)<>1
 OR NOT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-recovery-eligibility' AND resource_id=NEW.attempt_id AND actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='eligible'
 AND changes=jsonb_build_object('proof',encode(NEW.proof_hash,'hex'),'source',encode(NEW.source_hash,'hex')))
 OR NOT EXISTS(SELECT 1 FROM jobs WHERE event_key='scan-recover:'||NEW.attempt_id::text AND kind='payment.scan-recover' AND payload=jsonb_build_object('attempt_id',NEW.attempt_id::text))
 THEN RAISE EXCEPTION 'original barcode eligibility audit or job missing';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER scan_recovery_eligibility_source AFTER INSERT ON scan_recovery_eligibility DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION scan_recovery_eligibility_guard();

CREATE TABLE scan_recovery_query_events (
 id bigserial PRIMARY KEY,
 attempt_id bigint NOT NULL REFERENCES scan_recovery_eligibility(attempt_id),
 revision integer NOT NULL CHECK(revision BETWEEN 1 AND 12),
 kind text NOT NULL CHECK(kind IN('query','observed','error')),
 query_number integer NOT NULL CHECK(query_number BETWEEN 1 AND 6),
 query_claim_id bigint NOT NULL REFERENCES scan_recovery_query_events(id) DEFERRABLE INITIALLY DEFERRED,
 job_id bigint NOT NULL REFERENCES jobs(id),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 query_provider_no text NOT NULL,
 previous_hash bytea NOT NULL CHECK(octet_length(previous_hash)=32),
 receipt jsonb,
 created_at timestamptz NOT NULL,
 transaction_id text NOT NULL CHECK(transaction_id<>''),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(attempt_id,revision),UNIQUE(attempt_id,query_number,kind),
 CHECK((kind='query' AND revision=2*query_number-1 AND query_claim_id=id AND receipt IS NULL)
    OR(kind IN('observed','error') AND revision=2*query_number AND query_claim_id<>id
       AND ((kind='observed' AND receipt IS NOT NULL AND jsonb_typeof(receipt)='object') OR(kind='error' AND receipt IS NULL))))
);
CREATE TRIGGER scan_recovery_query_event_immutable BEFORE UPDATE OR DELETE ON scan_recovery_query_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE scan_recovery_query_heads (
 attempt_id bigint PRIMARY KEY REFERENCES scan_recovery_eligibility(attempt_id),
 eligibility_hash bytea NOT NULL CHECK(octet_length(eligibility_hash)=32),
 revision integer NOT NULL CHECK(revision BETWEEN 0 AND 12),
 event_id bigint REFERENCES scan_recovery_query_events(id),
 event_hash bytea,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK((revision=0 AND event_id IS NULL AND event_hash IS NULL) OR(revision>0 AND event_id IS NOT NULL AND event_hash IS NOT NULL AND octet_length(event_hash)=32))
);
CREATE FUNCTION scan_recovery_query_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='scan_recovery_query_heads' THEN
  IF NOT EXISTS(SELECT 1 FROM scan_recovery_eligibility e WHERE e.attempt_id=NEW.attempt_id AND e.proof_hash=NEW.eligibility_hash)
  OR (NEW.revision>0 AND NOT EXISTS(SELECT 1 FROM scan_recovery_query_events e WHERE (e.id,e.attempt_id,e.revision,e.proof_hash)=(NEW.event_id,NEW.attempt_id,NEW.revision,NEW.event_hash)))
  THEN RAISE EXCEPTION 'original barcode query head mismatch';END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM jobs j WHERE j.id=NEW.job_id AND j.event_key='scan-recover:'||NEW.attempt_id::text AND j.kind='payment.scan-recover' AND j.payload=jsonb_build_object('attempt_id',NEW.attempt_id::text))
  OR (SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-queries' AND resource_id=NEW.id)<>1
  OR NOT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-recovery-queries' AND resource_id=NEW.id AND actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation=NEW.kind
     AND changes=jsonb_build_object('attempt_id',NEW.attempt_id::text,'query_number',NEW.query_number::text,'kind',NEW.kind,'proof',encode(NEW.proof_hash,'hex')))
  THEN RAISE EXCEPTION 'original barcode query audit mismatch';END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER scan_recovery_query_event_source AFTER INSERT ON scan_recovery_query_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION scan_recovery_query_guard();
CREATE CONSTRAINT TRIGGER scan_recovery_query_head_source AFTER INSERT OR UPDATE ON scan_recovery_query_heads DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION scan_recovery_query_guard();

CREATE TABLE scan_cancellation_claims (
 attempt_id bigint PRIMARY KEY REFERENCES scan_recovery_origins(attempt_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 eligibility_hash bytea NOT NULL CHECK(octet_length(eligibility_hash)=32),
 query_head_hash bytea NOT NULL CHECK(octet_length(query_head_hash)=32),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 product text NOT NULL CHECK(product IN('alipay.trade.cancel','/secapi/pay/reverse')),
 provider_trade_no text NOT NULL,
 created_at timestamptz NOT NULL,
 transaction_id text NOT NULL CHECK(transaction_id<>''),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scan_cancellation_claim_immutable BEFORE UPDATE OR DELETE ON scan_cancellation_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE scan_cancellation_results (
 attempt_id bigint PRIMARY KEY REFERENCES scan_cancellation_claims(attempt_id),
 claim_hash bytea NOT NULL CHECK(octet_length(claim_hash)=32),
 state text NOT NULL CHECK(state IN('unknown','cancelled')),
 receipt jsonb,
 created_at timestamptz NOT NULL,
 transaction_id text NOT NULL CHECK(transaction_id<>''),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK((state='unknown' AND receipt IS NULL) OR(state='cancelled' AND receipt IS NOT NULL AND jsonb_typeof(receipt)='object'))
);
CREATE TRIGGER scan_cancellation_result_immutable BEFORE UPDATE OR DELETE ON scan_cancellation_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION scan_cancellation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='scan_cancellation_claims' THEN
  IF NOT EXISTS(SELECT 1 FROM scan_recovery_origins o JOIN scan_recovery_eligibility e ON e.attempt_id=o.attempt_id JOIN scan_recovery_query_heads h ON h.attempt_id=e.attempt_id
      WHERE o.attempt_id=NEW.attempt_id AND o.proof_hash=NEW.source_hash AND e.proof_hash=NEW.eligibility_hash AND h.proof_hash=NEW.query_head_hash AND h.revision=12
      AND NEW.created_at>=o.created_at+interval '15 seconds' AND NEW.created_at<o.expires_at)
  OR (SELECT count(*) FROM backoffice_audit WHERE resource='scan-cancellations' AND resource_id=NEW.attempt_id)<>1
  OR NOT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-cancellations' AND resource_id=NEW.attempt_id AND actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='claim'
      AND changes=jsonb_build_object('attempt_id',NEW.attempt_id::text,'proof',encode(NEW.proof_hash,'hex'),'query_head',encode(NEW.query_head_hash,'hex')))
  THEN RAISE EXCEPTION 'original barcode cancellation claim mismatch';END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM scan_cancellation_claims c JOIN scan_recovery_origins s ON s.attempt_id=c.attempt_id JOIN orders o ON o.id=s.order_id
      WHERE c.attempt_id=NEW.attempt_id AND c.proof_hash=NEW.claim_hash AND NEW.created_at>=c.created_at AND o.status=CASE WHEN NEW.state='cancelled' THEN 'closed' ELSE 'unknown' END)
  OR (SELECT count(*) FROM backoffice_audit WHERE resource='scan-cancellation-results' AND resource_id=NEW.attempt_id)<>1
  OR NOT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='scan-cancellation-results' AND resource_id=NEW.attempt_id AND actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation=NEW.state
      AND changes=jsonb_build_object('attempt_id',NEW.attempt_id::text,'proof',encode(NEW.proof_hash,'hex'),'claim',encode(NEW.claim_hash,'hex')))
  THEN RAISE EXCEPTION 'original barcode cancellation result mismatch';END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER scan_cancellation_claim_source AFTER INSERT ON scan_cancellation_claims DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION scan_cancellation_guard();
CREATE CONSTRAINT TRIGGER scan_cancellation_result_source AFTER INSERT ON scan_cancellation_results DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION scan_cancellation_guard();
