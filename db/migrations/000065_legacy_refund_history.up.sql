-- Historical claims are evidence of the sanitized export, never executable
-- refunds or proof of a provider outcome. Opening reserved obligations remain.
ALTER TABLE legacy_import_runs ADD COLUMN refund_history_version smallint NOT NULL DEFAULT 1 CHECK(refund_history_version IN (1,2));
ALTER TABLE legacy_import_runs ALTER COLUMN refund_history_version SET DEFAULT 2;
-- Birth ordering uses the top-level transaction identity, including across
-- released savepoints. Backfilled version1 values grant no history authority.
ALTER TABLE legacy_import_runs ADD COLUMN refund_history_birth_xid xid8 NOT NULL DEFAULT pg_current_xact_id();
CREATE TABLE legacy_refund_manifests (
 export_id text PRIMARY KEY REFERENCES legacy_import_runs(export_id) DEFERRABLE INITIALLY DEFERRED,
 source text NOT NULL CHECK(source IN ('epay_pro','epay_release')),
 source_hash text NOT NULL CHECK(source_hash~'^[0-9a-f]{64}$'),
 refund_count bigint NOT NULL CHECK(refund_count>=0),unknown_count bigint NOT NULL CHECK(unknown_count BETWEEN 0 AND refund_count),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER legacy_refund_manifest_immutable BEFORE UPDATE OR DELETE ON legacy_refund_manifests FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE legacy_refund_history (
 id bigserial PRIMARY KEY,
 export_id text NOT NULL REFERENCES legacy_import_runs(export_id) DEFERRABLE INITIALLY DEFERRED,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 order_id bigint NOT NULL REFERENCES orders(id),
 trade_no text NOT NULL CHECK(length(trade_no) BETWEEN 1 AND 128),
 refund_no text NOT NULL CHECK(length(refund_no) BETWEEN 1 AND 128),
 amount bigint NOT NULL CHECK(amount>0),
 legacy_status text NOT NULL CHECK(legacy_status IN ('succeeded','failed','unknown')),
 source_hash text NOT NULL CHECK(source_hash~'^[0-9a-f]{64}$'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(merchant_id,refund_no)
);
CREATE TRIGGER legacy_refund_history_immutable BEFORE UPDATE OR DELETE ON legacy_refund_history FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX legacy_refund_history_export ON legacy_refund_history(export_id,id);
CREATE INDEX legacy_refund_history_owner ON legacy_refund_history(merchant_id,id);
-- Children are written before either completion record, even within one
-- transaction. An immutable completed export can never acquire later children.
CREATE FUNCTION legacy_refund_history_birth() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM legacy_import_runs WHERE export_id=NEW.export_id)
 OR EXISTS(SELECT 1 FROM legacy_refund_manifests WHERE export_id=NEW.export_id)
 THEN RAISE EXCEPTION 'legacy refund export already sealed'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_refund_history_birth BEFORE INSERT ON legacy_refund_history FOR EACH ROW EXECUTE FUNCTION legacy_refund_history_birth();
CREATE FUNCTION legacy_refund_history_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM orders o WHERE (o.id,o.merchant_id,o.trade_no)=(NEW.order_id,NEW.merchant_id,NEW.trade_no))
 -- Require the parent to have been born in this transaction too. This closes
 -- a concurrent late-child insert that saw no parent at its BEFORE trigger.
 -- The explicit top-level xid also supports parent insertion in a savepoint.
 OR NOT EXISTS(SELECT 1 FROM legacy_import_runs r WHERE r.export_id=NEW.export_id AND r.sha256=NEW.source_hash AND r.refund_history_birth_xid=pg_current_xact_id())
 THEN RAISE EXCEPTION 'incomplete legacy refund history source'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_refund_history_complete AFTER INSERT ON legacy_refund_history DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION legacy_refund_history_complete();
CREATE FUNCTION legacy_import_history_birth() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.refund_history_birth_xid IS DISTINCT FROM pg_current_xact_id() THEN RAISE EXCEPTION 'invalid legacy import birth transaction'; END IF;
 IF NEW.refund_history_version<>2 THEN RAISE EXCEPTION 'new legacy import requires history manifest'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_import_history_birth BEFORE INSERT ON legacy_import_runs FOR EACH ROW EXECUTE FUNCTION legacy_import_history_birth();
CREATE FUNCTION legacy_refund_manifest_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target text;
BEGIN
 target:=NEW.export_id;
 IF NOT EXISTS(SELECT 1 FROM legacy_import_runs r JOIN legacy_refund_manifests m ON m.export_id=r.export_id WHERE r.export_id=target
  AND r.refund_history_version=2 AND (r.source,r.sha256)=(m.source,m.source_hash)
  AND m.refund_count=(SELECT count(*) FROM legacy_refund_history h WHERE h.export_id=target)
  AND m.unknown_count=(SELECT count(*) FROM legacy_refund_history h WHERE h.export_id=target AND h.legacy_status='unknown'))
 THEN RAISE EXCEPTION 'incomplete legacy refund manifest'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_import_manifest_complete AFTER INSERT ON legacy_import_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION legacy_refund_manifest_complete();
CREATE CONSTRAINT TRIGGER legacy_refund_manifest_complete AFTER INSERT ON legacy_refund_manifests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION legacy_refund_manifest_complete();
-- The birth/source guards prevent appending children after sealing, so these
-- two completion records validate counts a constant number of times/export.
