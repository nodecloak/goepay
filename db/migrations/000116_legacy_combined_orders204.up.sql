-- Old exports remain version zero: absence never invents a historical graph.
ALTER TABLE legacy_import_runs ADD COLUMN combined_history_version204 smallint NOT NULL DEFAULT 0 CHECK(combined_history_version204 IN (0,1));
ALTER TABLE legacy_import_runs ADD COLUMN combined_history_birth_xid204 xid8 NOT NULL DEFAULT pg_current_xact_id();

CREATE TABLE legacy_combined_export_manifests204 (
 export_id text PRIMARY KEY REFERENCES legacy_import_runs(export_id) DEFERRABLE INITIALLY DEFERRED,
 source text NOT NULL CHECK(source IN ('epay_pro','epay_release')),
 source_hash text NOT NULL CHECK(source_hash~'^[0-9a-f]{64}$'),
 exported_at text NOT NULL,
 parent_count bigint NOT NULL CHECK(parent_count>=0),
 child_count bigint NOT NULL CHECK(child_count>=0),
 parents_hash bytea NOT NULL CHECK(octet_length(parents_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER legacy_combined_export204_immutable BEFORE UPDATE OR DELETE ON legacy_combined_export_manifests204 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE legacy_combined_order_manifests204 (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 export_id text NOT NULL REFERENCES legacy_import_runs(export_id) DEFERRABLE INITIALLY DEFERRED,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 trade_no text NOT NULL UNIQUE,
 source_plugin text NOT NULL DEFAULT '',
 currency text NOT NULL CHECK(currency='CNY'),
 nominal_amount bigint NOT NULL CHECK(nominal_amount>0),
 charged_amount bigint NOT NULL CHECK(charged_amount>0),
 parent_settle smallint NOT NULL CHECK(parent_settle BETWEEN 0 AND 3),
 child_count integer NOT NULL CHECK(child_count BETWEEN 1 AND 50),
 children_amount bigint NOT NULL CHECK(children_amount=charged_amount),
 children_hash bytea NOT NULL CHECK(octet_length(children_hash)=32),
 source_hash text NOT NULL CHECK(source_hash~'^[0-9a-f]{64}$'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 birth_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
 UNIQUE(order_id,export_id,merchant_id,trade_no)
);
CREATE INDEX legacy_combined_order204_export ON legacy_combined_order_manifests204(export_id,order_id);
CREATE INDEX legacy_combined_order204_owner ON legacy_combined_order_manifests204(merchant_id,order_id);
CREATE TRIGGER legacy_combined_order204_immutable BEFORE UPDATE OR DELETE ON legacy_combined_order_manifests204 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE legacy_suborder_history204 (
 order_id bigint NOT NULL,
 export_id text NOT NULL,
 merchant_id bigint NOT NULL,
 trade_no text NOT NULL,
 position integer NOT NULL CHECK(position BETWEEN 1 AND 50),
 sub_trade_no text NOT NULL UNIQUE CHECK(length(sub_trade_no) BETWEEN 1 AND 25),
 api_trade_no text NOT NULL DEFAULT '' CHECK(length(api_trade_no)<=150),
 amount bigint NOT NULL CHECK(amount>0),
 refund_amount bigint CHECK(refund_amount BETWEEN 0 AND amount),
 legacy_status smallint NOT NULL CHECK(legacy_status BETWEEN 0 AND 4),
 settle smallint NOT NULL CHECK(settle BETWEEN 0 AND 1),
 source_hash text NOT NULL CHECK(source_hash~'^[0-9a-f]{64}$'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(order_id,position),
 FOREIGN KEY(order_id,export_id,merchant_id,trade_no) REFERENCES legacy_combined_order_manifests204(order_id,export_id,merchant_id,trade_no) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX legacy_suborder204_export ON legacy_suborder_history204(export_id,order_id,position);
CREATE TRIGGER legacy_suborder_history204_immutable BEFORE UPDATE OR DELETE ON legacy_suborder_history204 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION legacy_combined_birth204() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM legacy_import_runs WHERE export_id=NEW.export_id)
 OR EXISTS(SELECT 1 FROM legacy_combined_export_manifests204 WHERE export_id=NEW.export_id)
 THEN RAISE EXCEPTION 'combined history export already sealed'; END IF;
 IF TG_TABLE_NAME='legacy_suborder_history204' AND EXISTS(SELECT 1 FROM legacy_combined_order_manifests204 WHERE order_id=NEW.order_id)
 THEN RAISE EXCEPTION 'combined history parent already sealed'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER legacy_suborder204_birth BEFORE INSERT ON legacy_suborder_history204 FOR EACH ROW EXECUTE FUNCTION legacy_combined_birth204();
CREATE TRIGGER legacy_combined_order204_birth BEFORE INSERT ON legacy_combined_order_manifests204 FOR EACH ROW EXECUTE FUNCTION legacy_combined_birth204();

CREATE FUNCTION legacy_combined_child_complete204() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_combined_order_manifests204 m WHERE (m.order_id,m.export_id,m.merchant_id,m.trade_no,m.source_hash)=(NEW.order_id,NEW.export_id,NEW.merchant_id,NEW.trade_no,NEW.source_hash) AND m.birth_xid=pg_current_xact_id())
 THEN RAISE EXCEPTION 'combined child requires its original parent birth'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_suborder204_complete AFTER INSERT ON legacy_suborder_history204 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION legacy_combined_child_complete204();

CREATE FUNCTION legacy_combined_parent_complete204() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.birth_xid<>pg_current_xact_id()
 OR NOT EXISTS(SELECT 1 FROM orders o WHERE (o.id,o.merchant_id,o.trade_no,o.amount)=(NEW.order_id,NEW.merchant_id,NEW.trade_no,NEW.nominal_amount))
 OR NEW.child_count<>(SELECT count(*) FROM legacy_suborder_history204 c WHERE c.order_id=NEW.order_id)
 OR NEW.charged_amount<>(SELECT coalesce(sum(amount),0) FROM legacy_suborder_history204 c WHERE c.order_id=NEW.order_id)
 OR NOT EXISTS(SELECT 1 FROM legacy_import_runs r WHERE (r.export_id,r.sha256)=(NEW.export_id,NEW.source_hash) AND r.combined_history_version204=1 AND r.combined_history_birth_xid204=pg_current_xact_id())
 THEN RAISE EXCEPTION 'incomplete combined parent source'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_combined_order204_complete AFTER INSERT ON legacy_combined_order_manifests204 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION legacy_combined_parent_complete204();

CREATE FUNCTION legacy_combined_export_complete204() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target text;
BEGIN
 target:=NEW.export_id;
 IF NOT EXISTS(SELECT 1 FROM legacy_import_runs r JOIN legacy_combined_export_manifests204 m ON m.export_id=r.export_id
 WHERE r.export_id=target AND r.combined_history_version204=1 AND r.combined_history_birth_xid204=pg_current_xact_id()
 AND (r.source,r.sha256)=(m.source,m.source_hash)
 AND m.parent_count=(SELECT count(*) FROM legacy_combined_order_manifests204 p WHERE p.export_id=target)
 AND m.child_count=(SELECT count(*) FROM legacy_suborder_history204 c WHERE c.export_id=target))
 THEN RAISE EXCEPTION 'incomplete combined export manifest'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER legacy_combined_export204_complete AFTER INSERT ON legacy_combined_export_manifests204 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION legacy_combined_export_complete204();
CREATE CONSTRAINT TRIGGER legacy_import_combined204_complete AFTER INSERT ON legacy_import_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.combined_history_version204=1) EXECUTE FUNCTION legacy_combined_export_complete204();
