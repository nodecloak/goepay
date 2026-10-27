-- Independent display metadata only. No historical field is adopted, no row is
-- seeded, and no order, receipt, attempt, refund or financial proof is rewritten.
CREATE TABLE order_api_trade_alias_commits120 (
 order_id bigint NOT NULL REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 revision bigint NOT NULL CHECK(revision>0),
 commitment text NOT NULL CHECK(commitment ~ '^[a-f0-9]{64}$'),
 previous_commitment text NOT NULL CHECK(previous_commitment='' OR previous_commitment ~ '^[a-f0-9]{64}$'),
 audit_id bigint NOT NULL UNIQUE REFERENCES backoffice_audit(id),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_username text NOT NULL CHECK(octet_length(actor_username)>0),
 birth_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(order_id,revision)
);
CREATE TRIGGER order_api_trade_alias_commits120_immutable BEFORE UPDATE OR DELETE ON order_api_trade_alias_commits120 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER order_api_trade_alias_commits120_no_truncate BEFORE TRUNCATE ON order_api_trade_alias_commits120 FOR EACH STATEMENT EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE order_api_trade_aliases120 (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 revision bigint NOT NULL CHECK(revision>0),
 api_trade_no_alias text NOT NULL CHECK(octet_length(api_trade_no_alias)<=8192),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(order_id,revision) REFERENCES order_api_trade_alias_commits120(order_id,revision)
);
CREATE TRIGGER order_api_trade_aliases120_no_delete BEFORE DELETE ON order_api_trade_aliases120 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER order_api_trade_aliases120_no_truncate BEFORE TRUNCATE ON order_api_trade_aliases120 FOR EACH STATEMENT EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION guard_order_api_trade_alias120() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.facts->>'Version' IS DISTINCT FROM '1'
 OR NEW.facts->>'Revision' IS DISTINCT FROM NEW.revision::text
 OR NEW.facts->>'Alias' IS DISTINCT FROM NEW.api_trade_no_alias
 OR NEW.facts#>>'{Source,OrderID}' IS DISTINCT FROM NEW.order_id::text
 OR NEW.facts#>>'{Source,MerchantID}' IS DISTINCT FROM NEW.merchant_id::text
 OR (TG_OP='INSERT' AND NEW.revision<>1)
 OR (TG_OP='UPDATE' AND ((NEW.order_id,NEW.merchant_id) IS DISTINCT FROM (OLD.order_id,OLD.merchant_id) OR NEW.revision<>OLD.revision+1))
 OR NOT EXISTS(
  SELECT 1 FROM order_api_trade_alias_commits120 c JOIN backoffice_audit a ON a.id=c.audit_id
  WHERE c.order_id=NEW.order_id AND c.merchant_id=NEW.merchant_id AND c.revision=NEW.revision
   AND c.commitment=encode(NEW.proof_hash,'hex') AND c.birth_xid=pg_current_xact_id()
   AND a.resource='orders' AND a.resource_id=NEW.order_id AND a.operation='api-trade-alias-update'
   AND a.actor_role='admin' AND a.actor_id=c.actor_id AND a.actor_name=c.actor_username
   AND a.changes->>'revision'=c.revision::text AND a.changes->>'commitment'=c.commitment
   AND a.changes->>'previous_commitment'=c.previous_commitment
   AND a.changes->>'merchant_id'=NEW.merchant_id::text
   AND a.changes->>'trade_no'=(NEW.facts#>>'{Source,TradeNo}')
 ) THEN RAISE EXCEPTION 'invalid independent order display alias source'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_api_trade_aliases120_source BEFORE INSERT OR UPDATE ON order_api_trade_aliases120 FOR EACH ROW EXECUTE FUNCTION guard_order_api_trade_alias120();
CREATE INDEX order_api_trade_alias120_audit ON backoffice_audit(resource_id,id DESC) WHERE resource='orders' AND operation='api-trade-alias-update';
