-- A fee reward can be spent by its owner. A refund must first recover the
-- complete original reward into this refund's private, nonnegative escrow.
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold','settlement_reserve','channel_sharing_reserve','referral_clawback_reserve'));
ALTER TABLE accounts DROP CONSTRAINT accounts_scope_domain;
ALTER TABLE accounts ADD CONSTRAINT accounts_scope_domain CHECK(
 (kind IN ('settlement_reserve','channel_sharing_reserve') AND owner_id>0 AND scope_id>0)
 OR (kind='referral_clawback_reserve' AND owner_id=0 AND scope_id>0 AND balance>=0)
 OR (kind NOT IN ('settlement_reserve','channel_sharing_reserve','referral_clawback_reserve') AND scope_id=0));

CREATE TABLE referral_refund_clawbacks (
 refund_id bigint PRIMARY KEY REFERENCES refunds(id),
 reward_id bigint NOT NULL REFERENCES referral_rewards(id),
 order_id bigint NOT NULL REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 promoter_id bigint NOT NULL REFERENCES merchants(id),
 amount bigint NOT NULL CHECK(amount>0),
 original_fee bigint NOT NULL CHECK(original_fee>=amount),
 prior_fee_used bigint NOT NULL CHECK(prior_fee_used>=0 AND prior_fee_used<=original_fee-amount),
 refund_fee bigint NOT NULL CHECK(refund_fee>=0 AND refund_fee<=original_fee-prior_fee_used),
 fee_contribution bigint NOT NULL CHECK(fee_contribution>=0 AND fee_contribution<=amount AND fee_contribution<=refund_fee),
 escrow_account_id bigint NOT NULL UNIQUE REFERENCES accounts(id),
 promoter_account_id bigint NOT NULL REFERENCES accounts(id),
 fees_account_id bigint NOT NULL REFERENCES accounts(id),
 fee_reserved_account_id bigint NOT NULL REFERENCES accounts(id),
 reward_hash bytea NOT NULL CHECK(octet_length(reward_hash)=32),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','unknown','succeeded','failed')),
 reserve_journal_id bigint NOT NULL UNIQUE REFERENCES journals(id),
 contribution_journal_id bigint UNIQUE REFERENCES journals(id),
 restore_contribution_journal_id bigint UNIQUE REFERENCES journals(id),
 restore_reward_journal_id bigint UNIQUE REFERENCES journals(id),
 release_remainder_journal_id bigint UNIQUE REFERENCES journals(id),
 result_hash bytea CHECK(result_hash IS NULL OR octet_length(result_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 resolved_at timestamptz,
 CHECK(fee_contribution=GREATEST(0,refund_fee-(original_fee-amount-prior_fee_used))),
 CHECK((fee_contribution=0)=(contribution_journal_id IS NULL)),
 CHECK((state='pending')=(result_hash IS NULL)),
 CHECK((state IN ('succeeded','failed'))=(resolved_at IS NOT NULL))
);
CREATE UNIQUE INDEX referral_clawback_one_live_reward ON referral_refund_clawbacks(reward_id) WHERE state<>'failed';
CREATE INDEX referral_clawback_order_history ON referral_refund_clawbacks(order_id,refund_id);
CREATE TABLE referral_clawback_audits (
 refund_id bigint NOT NULL REFERENCES referral_refund_clawbacks(refund_id),
 state text NOT NULL CHECK(state IN ('pending','unknown','succeeded','failed')),
 event_key text NOT NULL UNIQUE,
 actor_role text NOT NULL CHECK(actor_role='system'),
 actor_name text NOT NULL CHECK(actor_name='refund-engine'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(refund_id,state)
);
CREATE TRIGGER referral_clawback_audit_immutable BEFORE UPDATE OR DELETE ON referral_clawback_audits FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION referral_clawback_update_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable original referral clawback'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'pending' OR NEW.result_hash IS NOT NULL THEN RAISE EXCEPTION 'clawback must begin pending'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.refund_id,NEW.reward_id,NEW.order_id,NEW.merchant_id,NEW.promoter_id,NEW.amount,NEW.original_fee,NEW.prior_fee_used,NEW.refund_fee,NEW.fee_contribution,NEW.escrow_account_id,NEW.promoter_account_id,NEW.fees_account_id,NEW.fee_reserved_account_id,NEW.reward_hash,NEW.request_hash,NEW.reserve_journal_id,NEW.contribution_journal_id,NEW.created_at)
 IS DISTINCT FROM (OLD.refund_id,OLD.reward_id,OLD.order_id,OLD.merchant_id,OLD.promoter_id,OLD.amount,OLD.original_fee,OLD.prior_fee_used,OLD.refund_fee,OLD.fee_contribution,OLD.escrow_account_id,OLD.promoter_account_id,OLD.fees_account_id,OLD.fee_reserved_account_id,OLD.reward_hash,OLD.request_hash,OLD.reserve_journal_id,OLD.contribution_journal_id,OLD.created_at)
 THEN RAISE EXCEPTION 'immutable original referral clawback'; END IF;
 IF OLD.state IN ('succeeded','failed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'terminal referral clawback'; END IF;
 IF OLD.state='unknown' AND NEW.state='pending' THEN RAISE EXCEPTION 'unknown referral clawback cannot be resent'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_clawback_update BEFORE INSERT OR UPDATE OR DELETE ON referral_refund_clawbacks FOR EACH ROW EXECUTE FUNCTION referral_clawback_update_guard();

CREATE FUNCTION referral_clawback_journal_valid(jid bigint,event text,source bigint,target bigint,value bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN value=0 THEN jid IS NULL ELSE jid IS NOT NULL AND EXISTS(
  SELECT 1 FROM journals j WHERE j.id=jid AND j.event_key=event
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
   AND (SELECT COALESCE(sum(e.amount),0)=-value FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=source)
   AND (SELECT COALESCE(sum(e.amount),0)=value FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=target)) END
$$;
CREATE VIEW referral_clawback_invariants AS
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
 AND referral_clawback_journal_valid((SELECT j.id FROM journals j WHERE j.event_key='refund:'||f.id||':reserve'),'refund:'||f.id||':reserve',(SELECT id FROM accounts WHERE owner_id=f.merchant_id AND kind='available' AND scope_id=0),(SELECT id FROM accounts WHERE owner_id=f.merchant_id AND kind='reserved' AND scope_id=0),f.merchant_amount)
 AND (f.status IN ('pending','unknown') OR referral_clawback_journal_valid((SELECT j.id FROM journals j WHERE j.event_key='refund:'||f.id||':resolve'),'refund:'||f.id||':resolve',(SELECT id FROM accounts WHERE owner_id=f.merchant_id AND kind='reserved' AND scope_id=0),(SELECT id FROM accounts WHERE scope_id=0 AND ((f.status='failed' AND owner_id=f.merchant_id AND kind='available') OR (f.status='succeeded' AND owner_id=0 AND kind='clearing'))),f.merchant_amount))
 AND referral_clawback_journal_valid((SELECT j.id FROM journals j WHERE j.event_key='refund-fee:'||f.id||':reserve'),'refund-fee:'||f.id||':reserve',fa.id,fh.id,f.fee_amount)
 AND (f.status IN ('pending','unknown') OR referral_clawback_journal_valid((SELECT j.id FROM journals j WHERE j.event_key='refund-fee:'||f.id||':resolve'),'refund-fee:'||f.id||':resolve',fh.id,CASE WHEN f.status='failed' THEN fa.id ELSE (SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0) END,f.fee_amount))
 AS valid
FROM referral_refund_clawbacks c
JOIN referral_rewards w ON w.id=c.reward_id JOIN orders o ON o.id=c.order_id JOIN refunds f ON f.id=c.refund_id
JOIN accounts a ON a.id=c.escrow_account_id JOIN accounts pa ON pa.id=c.promoter_account_id JOIN accounts fa ON fa.id=c.fees_account_id JOIN accounts fh ON fh.id=c.fee_reserved_account_id;

CREATE FUNCTION referral_clawback_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint; oid bigint;
BEGIN
 IF TG_TABLE_NAME='accounts' THEN
  IF NEW.kind<>'referral_clawback_reserve' THEN RETURN NULL; END IF; rid:=NEW.scope_id;
 ELSIF TG_TABLE_NAME='ledger_entries' THEN
  SELECT scope_id INTO rid FROM accounts WHERE id=NEW.account_id AND kind='referral_clawback_reserve'; IF rid IS NULL THEN RETURN NULL; END IF;
 ELSIF TG_TABLE_NAME='refunds' THEN rid:=NEW.id;
 ELSE rid:=NEW.refund_id; END IF;
 SELECT order_id INTO oid FROM refunds WHERE id=rid;
 IF EXISTS(SELECT 1 FROM referral_refund_clawbacks WHERE refund_id=rid) THEN
  IF NOT EXISTS(SELECT 1 FROM referral_clawback_invariants WHERE refund_id=rid AND valid IS TRUE) THEN RAISE EXCEPTION 'invalid referral clawback evidence'; END IF;
 ELSE
  IF TG_TABLE_NAME IN ('accounts','ledger_entries') OR EXISTS(SELECT 1 FROM referral_rewards w WHERE w.order_id=oid AND NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.reward_id=w.id AND c.state='succeeded' AND c.refund_id<>rid)) THEN RAISE EXCEPTION 'missing original referral clawback'; END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_clawback_valid AFTER INSERT OR UPDATE ON referral_refund_clawbacks DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_clawback_guard();
CREATE CONSTRAINT TRIGGER referral_refund_clawback_required AFTER INSERT OR UPDATE ON refunds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_clawback_guard();
CREATE CONSTRAINT TRIGGER referral_escrow_valid AFTER INSERT OR UPDATE ON accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.kind='referral_clawback_reserve') EXECUTE FUNCTION referral_clawback_guard();
CREATE CONSTRAINT TRIGGER referral_escrow_entry_valid AFTER INSERT ON ledger_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_clawback_guard();
CREATE FUNCTION referral_escrow_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.kind='referral_clawback_reserve' OR NEW.kind='referral_clawback_reserve') AND (NEW.owner_id,NEW.kind,NEW.scope_id) IS DISTINCT FROM (OLD.owner_id,OLD.kind,OLD.scope_id) THEN RAISE EXCEPTION 'immutable referral escrow identity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_escrow_identity BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION referral_escrow_identity_guard();
