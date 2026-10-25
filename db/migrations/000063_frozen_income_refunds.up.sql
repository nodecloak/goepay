-- Original income and held-refund reserves. Historical migrations stay frozen.
SET CONSTRAINTS account_balance_guard IMMEDIATE;
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold','settlement_reserve','channel_sharing_reserve','referral_clawback_reserve','order_income_reserve','order_income_refund_reserve'));
ALTER TABLE accounts DROP CONSTRAINT accounts_scope_domain;
ALTER TABLE accounts ADD CONSTRAINT accounts_scope_domain CHECK(kind IN ('settlement_reserve','channel_sharing_reserve','order_income_reserve','order_income_refund_reserve') AND owner_id>0 AND scope_id>0 OR kind='referral_clawback_reserve' AND owner_id=0 AND scope_id>0 OR kind NOT IN ('settlement_reserve','channel_sharing_reserve','referral_clawback_reserve','order_income_reserve','order_income_refund_reserve') AND scope_id=0);
SET CONSTRAINTS account_balance_guard DEFERRED;
ALTER TABLE order_income_holds ADD COLUMN funding_evidence_version integer NOT NULL DEFAULT 1 CHECK(funding_evidence_version IN(1,2));
ALTER TABLE order_income_holds ALTER COLUMN funding_evidence_version SET DEFAULT 2;
ALTER TABLE refunds ADD COLUMN funding_evidence_version integer NOT NULL DEFAULT 1 CHECK(funding_evidence_version IN(1,2));
ALTER TABLE refunds ALTER COLUMN funding_evidence_version SET DEFAULT 2;
CREATE FUNCTION income_funding_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.funding_evidence_version<>OLD.funding_evidence_version THEN RAISE EXCEPTION 'immutable income funding version';END IF;RETURN NEW;END $$;
CREATE TRIGGER income_hold_funding_version BEFORE UPDATE ON order_income_holds FOR EACH ROW EXECUTE FUNCTION income_funding_version_guard();
CREATE TRIGGER refund_funding_version BEFORE UPDATE ON refunds FOR EACH ROW EXECUTE FUNCTION income_funding_version_guard();
CREATE FUNCTION income_funding_birth_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.funding_evidence_version<>2 THEN RAISE EXCEPTION 'new income funding requires version two';END IF;RETURN NEW;END $$;
CREATE TRIGGER income_hold_birth_version BEFORE INSERT ON order_income_holds FOR EACH ROW EXECUTE FUNCTION income_funding_birth_version_guard();
CREATE TRIGGER refund_funding_birth_version BEFORE INSERT ON refunds FOR EACH ROW EXECUTE FUNCTION income_funding_birth_version_guard();
CREATE TABLE order_income_hold_fundings(
 hold_id bigint PRIMARY KEY REFERENCES order_income_holds(id),order_id bigint NOT NULL REFERENCES orders(id),merchant_id bigint NOT NULL REFERENCES merchants(id),account_id bigint NOT NULL UNIQUE REFERENCES accounts(id),amount bigint NOT NULL CHECK(amount>0),source_mode text NOT NULL CHECK(source_mode IN('fresh','legacy_adopt')),external_no text NOT NULL,
 funding_journal_id bigint NOT NULL REFERENCES journals(id),legacy_pool_journal_id bigint REFERENCES journals(id),freeze_actor_id bigint NOT NULL CHECK(freeze_actor_id>0),freeze_actor_name text NOT NULL,freeze_actor_class text NOT NULL CHECK(freeze_actor_class IN('super','operator')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,actor_class text NOT NULL CHECK(actor_class IN('super','operator')),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),CHECK(source_mode<>'legacy_adopt' OR actor_class='super')
);
CREATE TABLE order_income_hold_releases(
 hold_id bigint PRIMARY KEY REFERENCES order_income_hold_fundings(hold_id),amount bigint NOT NULL CHECK(amount>=0),consumed_amount bigint NOT NULL CHECK(consumed_amount>=0),journal_id bigint REFERENCES journals(id),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,actor_class text NOT NULL CHECK(actor_class IN('super','operator')),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),result_hash bytea NOT NULL CHECK(octet_length(result_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TABLE refund_fundings(
 refund_id bigint PRIMARY KEY REFERENCES refunds(id),order_id bigint NOT NULL REFERENCES orders(id),merchant_id bigint NOT NULL REFERENCES merchants(id),hold_id bigint REFERENCES order_income_hold_fundings(hold_id),hold_funding_hash bytea,
 merchant_amount bigint NOT NULL CHECK(merchant_amount>=0),hold_amount bigint NOT NULL CHECK(hold_amount>=0),available_amount bigint NOT NULL CHECK(available_amount>=0),fee_amount bigint NOT NULL CHECK(fee_amount>=0),reserve_account_id bigint NOT NULL REFERENCES accounts(id),hold_reserve_journal_id bigint REFERENCES journals(id),available_reserve_journal_id bigint REFERENCES journals(id),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),CHECK(hold_amount::numeric+available_amount=merchant_amount),CHECK(hold_id IS NOT NULL OR hold_amount=0 AND hold_funding_hash IS NULL),CHECK(hold_id IS NULL OR octet_length(hold_funding_hash)=32)
);
CREATE TABLE refund_funding_resolutions(
 refund_id bigint PRIMARY KEY REFERENCES refund_fundings(refund_id),state text NOT NULL CHECK(state IN('succeeded','failed')),held_return_journal_id bigint REFERENCES journals(id),available_return_journal_id bigint REFERENCES journals(id),consume_journal_id bigint REFERENCES journals(id),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),result_hash bytea NOT NULL CHECK(octet_length(result_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER income_funding_immutable BEFORE UPDATE OR DELETE ON order_income_hold_fundings FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER income_release_immutable BEFORE UPDATE OR DELETE ON order_income_hold_releases FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER refund_funding_immutable BEFORE UPDATE OR DELETE ON refund_fundings FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER refund_funding_result_immutable BEFORE UPDATE OR DELETE ON refund_funding_resolutions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX refund_funding_hold_history ON refund_fundings(hold_id,refund_id);
CREATE FUNCTION income_funding_account_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF (OLD.kind IN('order_income_reserve','order_income_refund_reserve') OR NEW.kind IN('order_income_reserve','order_income_refund_reserve')) AND (TG_OP='DELETE' OR (NEW.owner_id,NEW.kind,NEW.scope_id) IS DISTINCT FROM (OLD.owner_id,OLD.kind,OLD.scope_id)) THEN RAISE EXCEPTION 'immutable income reserve identity';END IF;RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;END $$;
CREATE TRIGGER income_reserve_identity BEFORE UPDATE OR DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION income_funding_account_identity();
CREATE FUNCTION refund_merchant_reserve_valid(rid bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN f.refund_id IS NULL THEN r.funding_evidence_version=1 AND referral_clawback_journal_valid((SELECT id FROM journals WHERE event_key='refund:'||r.id||':reserve'),'refund:'||r.id||':reserve',av.id,rv.id,r.merchant_amount)
 ELSE (f.order_id,f.merchant_id,f.merchant_amount,f.fee_amount)=(r.order_id,r.merchant_id,r.merchant_amount,r.fee_amount)
 AND referral_clawback_journal_valid(f.available_reserve_journal_id,'refund:'||r.id||':reserve',av.id,f.reserve_account_id,f.available_amount)
 AND referral_clawback_journal_valid(f.hold_reserve_journal_id,'refund-hold:'||r.id||':reserve',h.account_id,f.reserve_account_id,f.hold_amount)
 END FROM refunds r LEFT JOIN refund_fundings f ON f.refund_id=r.id LEFT JOIN order_income_hold_fundings h ON h.hold_id=f.hold_id JOIN accounts av ON(av.owner_id,av.kind,av.scope_id)=(r.merchant_id,'available',0) JOIN accounts rv ON(rv.owner_id,rv.kind,rv.scope_id)=(r.merchant_id,'reserved',0) WHERE r.id=rid
$$;
CREATE FUNCTION refund_merchant_resolution_valid(rid bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN r.status IN('pending','unknown') THEN NOT EXISTS(SELECT 1 FROM refund_funding_resolutions WHERE refund_id=r.id)
 WHEN f.refund_id IS NULL THEN r.funding_evidence_version=1 AND referral_clawback_journal_valid((SELECT id FROM journals WHERE event_key='refund:'||r.id||':resolve'),'refund:'||r.id||':resolve',rv.id,CASE WHEN r.status='failed' THEN av.id ELSE cl.id END,r.merchant_amount)
 ELSE z.state=r.status AND z.request_hash=f.request_hash AND
 CASE WHEN z.state='succeeded' THEN z.held_return_journal_id IS NULL AND z.available_return_journal_id IS NULL AND referral_clawback_journal_valid(z.consume_journal_id,'refund:'||r.id||':resolve',f.reserve_account_id,cl.id,r.merchant_amount)
 ELSE z.consume_journal_id IS NULL AND referral_clawback_journal_valid(z.held_return_journal_id,'refund-hold:'||r.id||':resolve',f.reserve_account_id,h.account_id,f.hold_amount) AND referral_clawback_journal_valid(z.available_return_journal_id,'refund:'||r.id||':resolve',f.reserve_account_id,av.id,f.available_amount) END
 END FROM refunds r LEFT JOIN refund_fundings f ON f.refund_id=r.id LEFT JOIN refund_funding_resolutions z ON z.refund_id=r.id LEFT JOIN order_income_hold_fundings h ON h.hold_id=f.hold_id JOIN accounts av ON(av.owner_id,av.kind,av.scope_id)=(r.merchant_id,'available',0) JOIN accounts rv ON(rv.owner_id,rv.kind,rv.scope_id)=(r.merchant_id,'reserved',0) JOIN accounts cl ON(cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0) WHERE r.id=rid
$$;
CREATE VIEW refund_funding_invariants AS SELECT f.refund_id,
 (f.order_id,f.merchant_id,f.merchant_amount,f.fee_amount)=(r.order_id,r.merchant_id,r.merchant_amount,r.fee_amount)
 AND (a.owner_id,a.kind,a.scope_id)=(r.merchant_id,CASE WHEN f.hold_id IS NULL THEN 'reserved' ELSE 'order_income_refund_reserve' END,CASE WHEN f.hold_id IS NULL THEN 0 ELSE r.id END)
 AND (f.hold_id IS NULL OR (h.order_id,h.merchant_id,h.request_hash)=(r.order_id,r.merchant_id,f.hold_funding_hash))
 AND (f.hold_id IS NULL OR a.balance=CASE WHEN r.status IN('pending','unknown') THEN r.merchant_amount ELSE 0 END)
 AND (f.hold_id IS NULL OR (SELECT count(*) FROM ledger_entries x WHERE x.account_id=a.id)=
 (CASE WHEN f.hold_amount>0 THEN 1 ELSE 0 END)+(CASE WHEN f.available_amount>0 THEN 1 ELSE 0 END)+
 CASE WHEN r.status IN('pending','unknown') THEN 0 WHEN r.status='succeeded' THEN CASE WHEN r.merchant_amount>0 THEN 1 ELSE 0 END ELSE (CASE WHEN f.hold_amount>0 THEN 1 ELSE 0 END)+(CASE WHEN f.available_amount>0 THEN 1 ELSE 0 END) END)
 AND (SELECT COALESCE(sum(q.amount),0) FROM refunds q WHERE q.order_id=r.order_id AND q.status<>'failed')<=o.amount
 AND (o.refund_fee_payer='merchant' AND r.fee_amount=0 OR o.refund_fee_payer='platform' AND (SELECT COALESCE(sum(q.merchant_amount),0) FROM refunds q WHERE q.order_id=r.order_id AND q.status<>'failed')<=o.merchant_amount AND (SELECT COALESCE(sum(q.fee_amount),0) FROM refunds q WHERE q.order_id=r.order_id AND q.status<>'failed')<=o.platform_fee)
 AND refund_merchant_reserve_valid(r.id) IS TRUE AND refund_merchant_resolution_valid(r.id) IS TRUE
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='refund-fundings' AND b.resource_id=r.id AND b.operation='reserve' AND (b.actor_role,b.actor_id,b.actor_name)=('system',0,'refund-engine'))
 AND (r.status IN('pending','unknown') OR EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='refund-fundings' AND b.resource_id=r.id AND b.operation=r.status AND (b.actor_role,b.actor_id,b.actor_name)=('system',0,'refund-engine')))
 AS valid FROM refund_fundings f JOIN refunds r ON r.id=f.refund_id JOIN orders o ON o.id=r.order_id JOIN accounts a ON a.id=f.reserve_account_id LEFT JOIN order_income_hold_fundings h ON h.hold_id=f.hold_id;
ALTER VIEW order_income_hold_funding_evidence RENAME TO order_income_hold_legacy_evidence;
CREATE VIEW order_income_hold_funding_evidence AS SELECT h.id,h.merchant_id,
 CASE WHEN f.hold_id IS NULL THEN h.funding_evidence_version=1 AND l.valid IS TRUE ELSE
 (f.order_id,f.merchant_id,f.amount)=(h.order_id,h.merchant_id,h.amount) AND (a.owner_id,a.kind,a.scope_id)=(h.merchant_id,'order_income_reserve',h.id)
 AND (f.source_mode='fresh' AND h.funding_evidence_version=2 AND f.legacy_pool_journal_id IS NULL AND referral_clawback_journal_valid(f.funding_journal_id,'order-income:'||h.id||':freeze',av.id,a.id,h.amount)
 OR f.source_mode='legacy_adopt' AND h.funding_evidence_version=1 AND referral_clawback_journal_valid(f.legacy_pool_journal_id,'order-income:'||h.id||':freeze',av.id,pool.id,h.amount) AND referral_clawback_journal_valid(f.funding_journal_id,'order-income:'||h.id||':adopt',pool.id,a.id,h.amount))
 AND EXISTS(SELECT 1 FROM order_income_hold_events e JOIN backoffice_audit b ON (b.actor_role,b.actor_id,b.actor_name)=('admin',e.actor_id,e.actor_name) AND b.resource='orders' AND b.resource_id=h.order_id AND b.operation='freeze-income' AND b.changes->>'hold_id'=h.id::text AND b.changes->>'amount'=h.amount::text WHERE e.hold_id=h.id AND e.action='freeze' AND (e.actor_id,e.actor_name,e.actor_class)=(f.freeze_actor_id,f.freeze_actor_name,f.freeze_actor_class))
 AND (f.source_mode='legacy_adopt' OR (f.freeze_actor_id,f.freeze_actor_name,f.freeze_actor_class)=(f.actor_id,f.actor_name,f.actor_class))
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='income-fundings' AND b.resource_id=h.id AND b.operation=f.source_mode AND (b.actor_role,b.actor_id,b.actor_name)=('admin',f.actor_id,f.actor_name))
 AND (SELECT COALESCE(sum(x.hold_amount),0) FROM refund_fundings x JOIN refunds r ON r.id=x.refund_id WHERE x.hold_id=h.id AND r.status<>'failed')<=h.amount
 AND a.balance=h.amount::numeric-(SELECT COALESCE(sum(x.hold_amount),0) FROM refund_fundings x JOIN refunds r ON r.id=x.refund_id WHERE x.hold_id=h.id AND r.status<>'failed')-COALESCE(rel.amount,0)
 AND (SELECT count(*) FROM ledger_entries x WHERE x.account_id=a.id)=1+
 (SELECT count(*) FROM refund_fundings x WHERE x.hold_id=h.id AND x.hold_amount>0)+
 (SELECT count(*) FROM refund_fundings x JOIN refunds r ON r.id=x.refund_id WHERE x.hold_id=h.id AND x.hold_amount>0 AND r.status='failed')+CASE WHEN rel.amount>0 THEN 1 ELSE 0 END
 AND CASE WHEN h.state='held' THEN rel.hold_id IS NULL ELSE rel.request_hash=f.request_hash AND rel.amount::numeric+rel.consumed_amount=h.amount AND NOT EXISTS(SELECT 1 FROM refund_fundings x JOIN refunds r ON r.id=x.refund_id WHERE x.hold_id=h.id AND r.status IN('pending','unknown')) AND referral_clawback_journal_valid(rel.journal_id,'order-income:'||h.id||':unfreeze',a.id,av.id,rel.amount)
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='income-fundings' AND b.resource_id=h.id AND b.operation='release' AND (b.actor_role,b.actor_id,b.actor_name)=('admin',rel.actor_id,rel.actor_name)) END
 END AS valid FROM order_income_holds h LEFT JOIN order_income_hold_legacy_evidence l ON l.id=h.id LEFT JOIN order_income_hold_fundings f ON f.hold_id=h.id LEFT JOIN order_income_hold_releases rel ON rel.hold_id=h.id LEFT JOIN accounts a ON a.id=f.account_id JOIN accounts av ON(av.owner_id,av.kind,av.scope_id)=(h.merchant_id,'available',0) JOIN accounts pool ON(pool.owner_id,pool.kind,pool.scope_id)=(h.merchant_id,'order_income_hold',0);
CREATE OR REPLACE FUNCTION order_income_holding_funding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;BEGIN IF TG_TABLE_NAME='accounts' THEN owner:=NEW.owner_id;ELSE owner:=NEW.merchant_id;END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts a WHERE(a.owner_id,a.kind,a.scope_id)=(owner,'order_income_hold',0) AND a.balance=(SELECT COALESCE(sum(h.amount),0) FROM order_income_holds h WHERE h.merchant_id=owner AND h.state='held' AND NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.hold_id=h.id))) OR EXISTS(SELECT 1 FROM order_income_holds h WHERE h.merchant_id=owner AND NOT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence v WHERE v.id=h.id AND v.valid IS TRUE)) THEN RAISE EXCEPTION 'income hold lacks isolated original funding';END IF;RETURN NULL;END $$;
CREATE OR REPLACE FUNCTION block_refund_while_income_held() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=NEW.order_id AND h.state='held' AND NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.hold_id=h.id)) THEN RAISE EXCEPTION 'income_hold_source_unprepared';END IF;RETURN NEW;END $$;
CREATE FUNCTION refund_funding_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE active bigint;BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;SELECT id INTO active FROM order_income_holds WHERE order_id=NEW.order_id AND state='held';
 IF NEW.hold_id IS DISTINCT FROM active THEN RAISE EXCEPTION 'refund funding differs from original active hold';END IF;
 IF NEW.hold_id IS NOT NULL AND NEW.hold_amount>(SELECT h.amount::numeric-COALESCE(sum(x.hold_amount) FILTER(WHERE r.status<>'failed'),0) FROM order_income_holds h LEFT JOIN refund_fundings x ON x.hold_id=h.id LEFT JOIN refunds r ON r.id=x.refund_id WHERE h.id=NEW.hold_id GROUP BY h.id) THEN RAISE EXCEPTION 'refund exceeds own held principal';END IF;RETURN NEW;END $$;
CREATE TRIGGER refund_funding_birth BEFORE INSERT ON refund_fundings FOR EACH ROW EXECUTE FUNCTION refund_funding_birth_guard();
CREATE FUNCTION income_funding_complete_guard() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE hid bigint;rid bigint;BEGIN
 IF TG_TABLE_NAME='order_income_holds' THEN hid:=NEW.id;
 ELSIF TG_TABLE_NAME IN('order_income_hold_fundings','order_income_hold_releases') THEN hid:=NEW.hold_id;
 ELSIF TG_TABLE_NAME='refunds' THEN rid:=NEW.id;
 ELSIF TG_TABLE_NAME IN('refund_fundings','refund_funding_resolutions') THEN rid:=NEW.refund_id;
 ELSIF TG_TABLE_NAME='accounts' THEN IF NEW.kind='order_income_reserve' THEN hid:=NEW.scope_id;ELSIF NEW.kind='order_income_refund_reserve' THEN rid:=NEW.scope_id;ELSE RETURN NULL;END IF;
 ELSE SELECT CASE WHEN kind='order_income_reserve' THEN scope_id END,CASE WHEN kind='order_income_refund_reserve' THEN scope_id END INTO hid,rid FROM accounts WHERE id=NEW.account_id;END IF;
 IF rid IS NOT NULL THEN
 IF EXISTS(SELECT 1 FROM refunds WHERE id=rid AND funding_evidence_version=2) AND NOT EXISTS(SELECT 1 FROM refund_funding_invariants WHERE refund_id=rid AND valid IS TRUE) THEN RAISE EXCEPTION 'missing valid refund source funding';END IF;
 IF TG_TABLE_NAME IN('accounts','ledger_entries') AND NOT EXISTS(SELECT 1 FROM refund_fundings WHERE refund_id=rid) THEN RAISE EXCEPTION 'orphan income refund account';END IF;
 SELECT hold_id INTO hid FROM refund_fundings WHERE refund_id=rid;END IF;
 IF hid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence WHERE id=hid AND valid IS TRUE) THEN RAISE EXCEPTION 'invalid isolated income funding';END IF;RETURN NULL;END $$;
CREATE CONSTRAINT TRIGGER income_funding_complete AFTER INSERT ON order_income_hold_fundings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER income_release_complete AFTER INSERT ON order_income_hold_releases DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER income_hold_complete AFTER INSERT OR UPDATE ON order_income_holds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER refund_funding_complete AFTER INSERT ON refund_fundings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER refund_funding_result_complete AFTER INSERT ON refund_funding_resolutions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER refund_source_required AFTER INSERT OR UPDATE ON refunds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER income_scoped_account_complete AFTER INSERT OR UPDATE ON accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.kind IN('order_income_reserve','order_income_refund_reserve')) EXECUTE FUNCTION income_funding_complete_guard();
CREATE CONSTRAINT TRIGGER income_scoped_entry_complete AFTER INSERT ON ledger_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION income_funding_complete_guard();
CREATE OR REPLACE VIEW order_income_source_evidence AS
SELECT o.id AS order_id,o.merchant_id,o.merchant_amount AS merchant_net,
 r.gross AS refund_gross,r.amount AS refund_merchant_amount,r.pending AS pending_refunds,
 p.amount AS channel_sharing_amount,a.amount AS internal_allocated_amount,
 GREATEST(0::numeric,o.merchant_amount::numeric-r.amount-p.amount-a.amount)::bigint AS remaining,
 o.status='paid' AND o.merchant_amount>0 AND NOT EXISTS(SELECT 1 FROM recharge_intents z WHERE z.order_id=o.id)
 AND r.gross<=o.amount AND r.valid AND a.valid
 AND EXISTS(SELECT 1 FROM journals j WHERE j.event_key='payment:'||o.trade_no
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=o.merchant_amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts cl ON cl.id=e.account_id WHERE e.journal_id=j.id AND (cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0))=-o.merchant_amount)
 AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=f.order_id AND v.valid IS TRUE))
 AND NOT EXISTS(SELECT 1 FROM provider_attempt_sharing d WHERE d.order_id=o.id AND d.declared AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=d.order_id)) AS valid
FROM orders o
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(f.amount) FILTER(WHERE f.status='succeeded'),0)::bigint AS gross,
 COALESCE(sum(f.merchant_amount) FILTER(WHERE f.status='succeeded'),0)::bigint AS amount,
 count(*) FILTER(WHERE f.status IN ('pending','unknown')) AS pending,
 COALESCE(bool_and(f.status<>'succeeded' OR refund_merchant_reserve_valid(f.id) IS TRUE AND refund_merchant_resolution_valid(f.id) IS TRUE),true) AS valid
 FROM refunds f WHERE f.order_id=o.id
) r
CROSS JOIN LATERAL (
 SELECT order_income_channel_sharing_outflow(o.id) AS amount
) p
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(i.amount) FILTER(WHERE i.status<>'rejected'),0)::bigint AS amount,COALESCE(bool_and(i.valid IS TRUE),true) AS valid
 FROM order_income_internal_allocation_evidence i WHERE i.order_id=o.id
) a;
CREATE OR REPLACE VIEW referral_clawback_invariants AS
SELECT c.refund_id,
 (c.reward_id,c.order_id,c.merchant_id,c.promoter_id,c.amount,c.original_fee,c.reward_hash)=(w.id,w.order_id,w.merchant_id,w.promoter_id,w.amount,o.platform_fee,w.proof_hash)
 AND EXISTS(SELECT 1 FROM referral_reward_invariants v WHERE v.reward_id=w.id AND v.valid IS TRUE)
 AND (f.order_id,f.merchant_id,f.fee_amount,f.status)=(c.order_id,c.merchant_id,c.refund_fee,c.state)
 AND (a.owner_id,a.kind,a.scope_id)=(0,'referral_clawback_reserve',c.refund_id)
 AND (pa.owner_id,pa.kind,pa.scope_id)=(c.promoter_id,'available',0)
 AND (fa.owner_id,fa.kind,fa.scope_id)=(0,'fees',0)
 AND (fh.owner_id,fh.kind,fh.scope_id)=(0,'fee_reserved',0)
 AND a.balance=CASE WHEN c.state IN ('pending','unknown') THEN c.amount-c.fee_contribution ELSE 0 END
 AND (SELECT count(*) FROM ledger_entries e WHERE e.account_id=a.id)=
   1+(CASE WHEN c.fee_contribution>0 THEN 1 ELSE 0 END)
   +(CASE WHEN c.state='failed' THEN 1+(CASE WHEN c.fee_contribution>0 THEN 1 ELSE 0 END) WHEN c.state='succeeded' AND c.amount>c.fee_contribution THEN 1 ELSE 0 END)
 AND referral_clawback_journal_valid(c.reserve_journal_id,'referral-clawback:'||c.refund_id||':reserve',pa.id,a.id,c.amount)
 AND referral_clawback_journal_valid(c.contribution_journal_id,'referral-clawback:'||c.refund_id||':fee-contribution',a.id,fa.id,c.fee_contribution)
 AND CASE WHEN c.state='failed' THEN
  referral_clawback_journal_valid(c.restore_contribution_journal_id,'referral-clawback:'||c.refund_id||':restore-contribution',fa.id,a.id,c.fee_contribution)
  AND referral_clawback_journal_valid(c.restore_reward_journal_id,'referral-clawback:'||c.refund_id||':restore-reward',a.id,pa.id,c.amount)
  AND c.release_remainder_journal_id IS NULL
 WHEN c.state='succeeded' THEN
  referral_clawback_journal_valid(c.release_remainder_journal_id,'referral-clawback:'||c.refund_id||':release-remainder',a.id,fa.id,c.amount-c.fee_contribution)
  AND c.restore_contribution_journal_id IS NULL AND c.restore_reward_journal_id IS NULL
 ELSE c.restore_contribution_journal_id IS NULL AND c.restore_reward_journal_id IS NULL AND c.release_remainder_journal_id IS NULL END
 AND EXISTS(SELECT 1 FROM referral_clawback_audits b WHERE b.refund_id=c.refund_id AND b.state='pending' AND b.event_key='referral-clawback:'||c.refund_id||':pending' AND b.proof_hash=c.request_hash)
 AND (c.state='pending' OR EXISTS(SELECT 1 FROM referral_clawback_audits b WHERE b.refund_id=c.refund_id AND b.state=c.state AND b.event_key='referral-clawback:'||c.refund_id||':'||c.state AND b.proof_hash=c.result_hash))
 AND refund_merchant_reserve_valid(f.id) IS TRUE AND refund_merchant_resolution_valid(f.id) IS TRUE
 AND referral_clawback_journal_valid((SELECT j.id FROM journals j WHERE j.event_key='refund-fee:'||f.id||':reserve'),'refund-fee:'||f.id||':reserve',fa.id,fh.id,f.fee_amount)
 AND (f.status IN ('pending','unknown') OR referral_clawback_journal_valid((SELECT j.id FROM journals j WHERE j.event_key='refund-fee:'||f.id||':resolve'),'refund-fee:'||f.id||':resolve',fh.id,CASE WHEN f.status='failed' THEN fa.id ELSE (SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0) END,f.fee_amount))
 AS valid
FROM referral_refund_clawbacks c
JOIN referral_rewards w ON w.id=c.reward_id JOIN orders o ON o.id=c.order_id JOIN refunds f ON f.id=c.refund_id
JOIN accounts a ON a.id=c.escrow_account_id JOIN accounts pa ON pa.id=c.promoter_account_id JOIN accounts fa ON fa.id=c.fees_account_id JOIN accounts fh ON fh.id=c.fee_reserved_account_id;
CREATE OR REPLACE VIEW provider_refund_result_invariants AS
SELECT e.refund_id,
 (r.order_id,r.amount,r.refund_no,r.status)=(e.order_id,e.amount,e.refund_no,e.state)
 AND (e.source='internal' OR (p.kind,p.order_id,p.refund_id,p.channel_id,p.state)=('refund',e.order_id,e.refund_id,e.channel_id,e.state))
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
