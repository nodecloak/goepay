-- New, explicit deferred per-order entitlements; never rewrite v1 proofs.
ALTER TABLE referral_policies DROP CONSTRAINT referral_policies_mode_check;
ALTER TABLE referral_policies ADD CONSTRAINT referral_policies_mode_check CHECK(mode IN('immediate','daily'));
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold','settlement_reserve','channel_sharing_reserve','referral_clawback_reserve','order_income_reserve','order_income_refund_reserve','referral_daily_pending','referral_daily_refund_reserve'));
ALTER TABLE accounts DROP CONSTRAINT accounts_scope_domain;
ALTER TABLE accounts ADD CONSTRAINT accounts_scope_domain CHECK(
 kind IN('settlement_reserve','channel_sharing_reserve','order_income_reserve','order_income_refund_reserve') AND owner_id>0 AND scope_id>0
 OR kind IN('referral_clawback_reserve','referral_daily_pending','referral_daily_refund_reserve') AND owner_id=0 AND scope_id>0
 OR kind NOT IN('settlement_reserve','channel_sharing_reserve','referral_clawback_reserve','order_income_reserve','order_income_refund_reserve','referral_daily_pending','referral_daily_refund_reserve') AND scope_id=0);
ALTER TABLE accounts ADD CONSTRAINT referral_daily_nonnegative CHECK(kind NOT IN('referral_daily_pending','referral_daily_refund_reserve') OR balance>=0);
CREATE TABLE referral_daily_terms(
 order_id bigint PRIMARY KEY REFERENCES referral_order_terms(order_id),
 source_version integer NOT NULL DEFAULT 2 CHECK(source_version=2),mode text NOT NULL DEFAULT 'daily' CHECK(mode='daily'),
 timezone text NOT NULL DEFAULT 'Asia/Shanghai-UTC+08' CHECK(timezone='Asia/Shanghai-UTC+08'),
 merchant_id bigint NOT NULL REFERENCES merchants(id),promoter_id bigint NOT NULL REFERENCES merchants(id),
 amount bigint NOT NULL CHECK(amount>0),platform_fee bigint NOT NULL CHECK(platform_fee>0 AND platform_fee<=amount),reward_amount bigint NOT NULL CHECK(reward_amount>0 AND reward_amount<=platform_fee),
 basis text NOT NULL CHECK(basis IN('fee','gross')),policy_versions jsonb NOT NULL CHECK(jsonb_typeof(policy_versions)='array' AND jsonb_array_length(policy_versions)=5),
 pending_account_id bigint NOT NULL UNIQUE REFERENCES accounts(id),terms_hash bytea NOT NULL CHECK(octet_length(terms_hash)=32),source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),CHECK(merchant_id<>promoter_id)
);
CREATE TRIGGER referral_daily_terms_immutable BEFORE UPDATE OR DELETE ON referral_daily_terms FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_referral_daily_terms_birth() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM orders o JOIN referral_order_terms t ON t.order_id=o.id JOIN accounts a ON a.id=NEW.pending_account_id WHERE o.id=NEW.order_id AND o.status='created' AND o.created_at=transaction_timestamp()
 AND (o.merchant_id,o.amount,o.platform_fee)=(NEW.merchant_id,NEW.amount,NEW.platform_fee)
 AND t.enabled AND (t.promoter_id,t.reward_amount,t.policy_versions,t.proof_hash)=(NEW.promoter_id,NEW.reward_amount,NEW.policy_versions,NEW.terms_hash)
 AND (a.owner_id,a.kind,a.scope_id,a.balance)=(0::bigint,'referral_daily_pending',o.id,0::bigint)) THEN RAISE EXCEPTION 'daily terms require original created-order source';END IF;RETURN NEW;END $$;
CREATE TRIGGER referral_daily_terms_birth BEFORE INSERT ON referral_daily_terms FOR EACH ROW EXECUTE FUNCTION guard_referral_daily_terms_birth();
CREATE TABLE referral_daily_liabilities(
 order_id bigint PRIMARY KEY REFERENCES referral_daily_terms(order_id),activated_at timestamptz NOT NULL,business_day date NOT NULL,eligible_day date NOT NULL,
 channel_id bigint NOT NULL CHECK(channel_id>0),provider_trade_no text NOT NULL CHECK(length(provider_trade_no) BETWEEN 1 AND 256),
 fees_account_id bigint NOT NULL REFERENCES accounts(id),pending_account_id bigint NOT NULL REFERENCES accounts(id),funding_journal_id bigint NOT NULL UNIQUE REFERENCES journals(id),source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 CHECK(business_day=((activated_at AT TIME ZONE 'UTC')+interval '8 hours')::date),CHECK(eligible_day=business_day+1)
);
CREATE TRIGGER referral_daily_liability_immutable BEFORE UPDATE OR DELETE ON referral_daily_liabilities FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE referral_daily_events(
 id bigserial PRIMARY KEY,order_id bigint NOT NULL REFERENCES referral_daily_terms(order_id),
 operation text NOT NULL CHECK(operation IN('birth','funded','awarded','refund-pending','refund-unknown','refund-failed','refund-succeeded')),
 reference_id bigint NOT NULL CHECK(reference_id>=0),event_key text NOT NULL UNIQUE,actor_role text NOT NULL DEFAULT 'system' CHECK(actor_role='system'),
 actor_name text NOT NULL CHECK(actor_name IN('payment-engine','daily-referral-engine','refund-engine')),subject_hash bytea NOT NULL CHECK(octet_length(subject_hash)=32),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(order_id,operation,reference_id)
);
CREATE TRIGGER referral_daily_event_immutable BEFORE UPDATE OR DELETE ON referral_daily_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX ledger_entries_by_journal ON ledger_entries(journal_id);
CREATE VIEW referral_daily_liability_sources AS
SELECT l.order_id,
 o.status='paid' AND b.enabled AND b.purpose='payment'
 AND (d.merchant_id,d.promoter_id,d.amount,d.platform_fee,d.reward_amount,d.terms_hash,d.policy_versions)=(o.merchant_id,b.promoter_id,o.amount,o.platform_fee,b.reward_amount,b.proof_hash,b.policy_versions)
 AND d.source_version=2 AND d.mode='daily' AND d.timezone='Asia/Shanghai-UTC+08'
 AND (p.id,p.owner_id,p.kind,p.scope_id)=(d.pending_account_id,0::bigint,'referral_daily_pending',o.id)
 AND l.pending_account_id=p.id AND f.owner_id=0 AND f.kind='fees' AND f.scope_id=0
 AND l.activated_at>=o.created_at AND l.business_day=((l.activated_at AT TIME ZONE 'UTC')+interval '8 hours')::date AND l.eligible_day=l.business_day+1
 AND (v.channel_id,v.provider_trade_no)=(l.channel_id,l.provider_trade_no)
 AND j.event_key='referral-daily-funding:'||o.id::text
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-d.reward_amount FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=f.id)
 AND (SELECT COALESCE(sum(e.amount),0)=d.reward_amount FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=p.id)
 AND EXISTS(SELECT 1 FROM journals fj WHERE fj.event_key='payment-fee:'||o.trade_no AND fj.transaction_id=j.transaction_id
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=fj.id)
 AND (SELECT COALESCE(sum(e.amount),0)=o.platform_fee FROM ledger_entries e WHERE e.journal_id=fj.id AND e.account_id=f.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-o.platform_fee FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=fj.id AND a.owner_id=0 AND a.kind='clearing' AND a.scope_id=0))
 AND EXISTS(SELECT 1 FROM referral_daily_events e WHERE e.order_id=o.id AND e.operation='funded' AND e.reference_id=0 AND e.actor_name='payment-engine') AS valid
FROM referral_daily_liabilities l JOIN referral_daily_terms d ON d.order_id=l.order_id JOIN referral_order_terms b ON b.order_id=l.order_id JOIN orders o ON o.id=l.order_id
JOIN accounts p ON p.id=l.pending_account_id JOIN accounts f ON f.id=l.fees_account_id JOIN journals j ON j.id=l.funding_journal_id JOIN verified_payment_receipts v ON v.order_id=l.order_id;
CREATE FUNCTION guard_referral_daily_activation_tx() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM journals j WHERE j.id=NEW.funding_journal_id AND j.transaction_id=pg_current_xact_id()) THEN RAISE EXCEPTION 'daily activation requires original funding transaction';END IF;RETURN NEW;END $$;
CREATE TRIGGER referral_daily_activation_tx BEFORE INSERT ON referral_daily_liabilities FOR EACH ROW EXECUTE FUNCTION guard_referral_daily_activation_tx();
CREATE FUNCTION guard_referral_daily_activation_source() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_daily_liability_sources WHERE order_id=NEW.order_id AND valid IS TRUE) THEN RAISE EXCEPTION 'daily activation source or audit invalid';END IF;RETURN NULL;END $$;
CREATE CONSTRAINT TRIGGER referral_daily_activation_source AFTER INSERT ON referral_daily_liabilities DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_referral_daily_activation_source();
CREATE INDEX referral_daily_due ON referral_daily_liabilities(eligible_day,order_id);
CREATE INDEX referral_daily_owner ON referral_daily_terms(promoter_id,order_id);
CREATE TABLE referral_daily_awards(
 order_id bigint PRIMARY KEY REFERENCES referral_daily_liabilities(order_id),reward_id bigint NOT NULL UNIQUE REFERENCES referral_rewards(id),
 release_journal_id bigint NOT NULL UNIQUE REFERENCES journals(id),executed_at timestamptz NOT NULL,source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32)
);
CREATE TRIGGER referral_daily_award_immutable BEFORE UPDATE OR DELETE ON referral_daily_awards FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE VIEW referral_daily_award_invariants AS SELECT a.order_id,
 src.valid IS TRUE AND old.valid IS TRUE AND r.order_id=a.order_id AND r.amount=d.reward_amount
 AND j.event_key='referral-daily-release:'||a.order_id::text AND j.transaction_id=awardj.transaction_id
 AND ((a.executed_at AT TIME ZONE 'UTC')+interval '8 hours')::date>=l.eligible_day
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-d.reward_amount FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=d.pending_account_id)
 AND (SELECT COALESCE(sum(e.amount),0)=d.reward_amount FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=l.fees_account_id)
 AND EXISTS(SELECT 1 FROM referral_daily_events e WHERE e.order_id=a.order_id AND e.operation='awarded' AND e.reference_id=r.id AND e.actor_name='daily-referral-engine') AS valid
 FROM referral_daily_awards a JOIN referral_daily_liabilities l ON l.order_id=a.order_id JOIN referral_daily_terms d ON d.order_id=a.order_id
 JOIN referral_daily_liability_sources src ON src.order_id=a.order_id JOIN referral_rewards r ON r.id=a.reward_id JOIN referral_reward_invariants old ON old.reward_id=r.id
 JOIN journals j ON j.id=a.release_journal_id JOIN journals awardj ON awardj.id=r.journal_id;
CREATE FUNCTION guard_referral_daily_award() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_daily_award_invariants WHERE order_id=NEW.order_id AND valid IS TRUE) THEN RAISE EXCEPTION 'daily award lacks original funded release or audit';END IF;RETURN NULL;END $$;
CREATE CONSTRAINT TRIGGER referral_daily_award_source AFTER INSERT ON referral_daily_awards DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_referral_daily_award();
CREATE FUNCTION guard_original_daily_reward() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF EXISTS(SELECT 1 FROM referral_order_terms t JOIN referral_policies p ON p.id=COALESCE(NULLIF((t.policy_versions->>2)::bigint,0),NULLIF((t.policy_versions->>1)::bigint,0),NULLIF((t.policy_versions->>0)::bigint,0)) WHERE t.order_id=NEW.order_id AND t.enabled AND p.mode='daily')
 AND NOT EXISTS(SELECT 1 FROM referral_daily_award_invariants WHERE order_id=NEW.order_id AND valid IS TRUE) THEN RAISE EXCEPTION 'original daily policy cannot award without deferred source';END IF;RETURN NULL;END $$;
CREATE CONSTRAINT TRIGGER referral_daily_original_reward AFTER INSERT ON referral_rewards DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_original_daily_reward();
CREATE TABLE referral_daily_refund_reservations(
 refund_id bigint PRIMARY KEY REFERENCES refunds(id),order_id bigint NOT NULL REFERENCES referral_daily_liabilities(order_id),
 amount bigint NOT NULL CHECK(amount>0),original_fee bigint NOT NULL CHECK(original_fee>=amount),refund_fee bigint NOT NULL CHECK(refund_fee BETWEEN 0 AND original_fee),fee_contribution bigint NOT NULL CHECK(fee_contribution BETWEEN 0 AND amount),
 pending_account_id bigint NOT NULL REFERENCES accounts(id),escrow_account_id bigint NOT NULL UNIQUE REFERENCES accounts(id),fees_account_id bigint NOT NULL REFERENCES accounts(id),fee_reserved_account_id bigint NOT NULL REFERENCES accounts(id),
 reserve_journal_id bigint NOT NULL UNIQUE REFERENCES journals(id),contribution_journal_id bigint UNIQUE REFERENCES journals(id),restore_contribution_journal_id bigint UNIQUE REFERENCES journals(id),restore_pending_journal_id bigint UNIQUE REFERENCES journals(id),release_remainder_journal_id bigint UNIQUE REFERENCES journals(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','unknown','failed','succeeded')),result_hash bytea,previous_result_hash bytea,result_at timestamptz,created_at timestamptz NOT NULL,
 CHECK(fee_contribution=GREATEST(0,refund_fee-(original_fee-amount))),CHECK((fee_contribution=0)=(contribution_journal_id IS NULL)),
 CHECK((state='pending')=(result_hash IS NULL)),CHECK((state='pending')=(result_at IS NULL)),CHECK(result_hash IS NULL OR octet_length(result_hash)=32),CHECK(previous_result_hash IS NULL OR octet_length(previous_result_hash)=32),CHECK(state IN('failed','succeeded') OR previous_result_hash IS NULL),
 CHECK(state='failed' AND restore_pending_journal_id IS NOT NULL AND (fee_contribution=0)=(restore_contribution_journal_id IS NULL) AND release_remainder_journal_id IS NULL
 OR state='succeeded' AND restore_pending_journal_id IS NULL AND restore_contribution_journal_id IS NULL AND (amount=fee_contribution)=(release_remainder_journal_id IS NULL)
 OR state IN('pending','unknown') AND restore_pending_journal_id IS NULL AND restore_contribution_journal_id IS NULL AND release_remainder_journal_id IS NULL)
);
CREATE UNIQUE INDEX referral_daily_one_unresolved_refund ON referral_daily_refund_reservations(order_id) WHERE state IN('pending','unknown');
CREATE UNIQUE INDEX referral_daily_one_success_refund ON referral_daily_refund_reservations(order_id) WHERE state='succeeded';
CREATE TRIGGER referral_daily_refund_no_delete BEFORE DELETE ON referral_daily_refund_reservations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_referral_daily_refund_transition() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN
 IF NEW.state<>'pending' OR NEW.result_hash IS NOT NULL OR NEW.previous_result_hash IS NOT NULL OR NEW.result_at IS NOT NULL OR NOT EXISTS(SELECT 1 FROM journals j WHERE j.id=NEW.reserve_journal_id AND j.transaction_id=pg_current_xact_id()) THEN RAISE EXCEPTION 'daily refund requires original funded claim';END IF;
 ELSE
 IF (NEW.refund_id,NEW.order_id,NEW.amount,NEW.original_fee,NEW.refund_fee,NEW.fee_contribution,NEW.pending_account_id,NEW.escrow_account_id,NEW.fees_account_id,NEW.fee_reserved_account_id,NEW.reserve_journal_id,NEW.contribution_journal_id,NEW.request_hash,NEW.created_at)
 IS DISTINCT FROM (OLD.refund_id,OLD.order_id,OLD.amount,OLD.original_fee,OLD.refund_fee,OLD.fee_contribution,OLD.pending_account_id,OLD.escrow_account_id,OLD.fees_account_id,OLD.fee_reserved_account_id,OLD.reserve_journal_id,OLD.contribution_journal_id,OLD.request_hash,OLD.created_at)
 OR OLD.state NOT IN('pending','unknown') OR NEW.state NOT IN('unknown','failed','succeeded') OR NEW.state=OLD.state OR NEW.previous_result_hash IS DISTINCT FROM OLD.result_hash THEN RAISE EXCEPTION 'immutable daily refund source or terminal result';END IF;
 END IF;RETURN NEW;END $$;
CREATE TRIGGER referral_daily_refund_transition BEFORE INSERT OR UPDATE ON referral_daily_refund_reservations FOR EACH ROW EXECUTE FUNCTION guard_referral_daily_refund_transition();
CREATE FUNCTION referral_daily_journal_valid(jid bigint,src bigint,dst bigint,cents bigint,original_key text) RETURNS boolean LANGUAGE SQL STABLE AS $$ SELECT CASE WHEN cents=0 THEN jid IS NULL ELSE jid IS NOT NULL AND EXISTS(SELECT 1 FROM journals j WHERE j.id=jid AND j.event_key=original_key
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-cents FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=src)
 AND (SELECT COALESCE(sum(e.amount),0)=cents FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=dst)) END $$;
CREATE VIEW referral_daily_refund_invariants AS SELECT c.refund_id,
 src.valid IS TRUE AND (r.order_id,r.fee_amount,r.status)=(c.order_id,c.refund_fee,c.state)
 AND (c.amount,c.original_fee,c.pending_account_id,c.fees_account_id)=(d.reward_amount,d.platform_fee,d.pending_account_id,l.fees_account_id)
 AND es.owner_id=0 AND es.kind='referral_daily_refund_reserve' AND es.scope_id=r.id AND fr.owner_id=0 AND fr.kind='fee_reserved' AND fr.scope_id=0
 AND es.balance=CASE WHEN c.state IN('pending','unknown') THEN c.amount-c.fee_contribution ELSE 0 END
 AND (c.state='failed' OR NOT EXISTS(SELECT 1 FROM referral_daily_awards a WHERE a.order_id=c.order_id))
 AND referral_daily_journal_valid(c.reserve_journal_id,c.pending_account_id,c.escrow_account_id,c.amount,'referral-daily-refund:'||r.id::text||':reserve')
 AND referral_daily_journal_valid(c.contribution_journal_id,c.escrow_account_id,c.fees_account_id,c.fee_contribution,'referral-daily-refund:'||r.id::text||':fee-contribution')
 AND referral_daily_journal_valid(fj.id,c.fees_account_id,c.fee_reserved_account_id,c.refund_fee,'refund-fee:'||r.id::text||':reserve')
 AND (c.state<>'failed' OR referral_daily_journal_valid(c.restore_contribution_journal_id,c.fees_account_id,c.escrow_account_id,c.fee_contribution,'referral-daily-refund:'||r.id::text||':restore-contribution') AND referral_daily_journal_valid(c.restore_pending_journal_id,c.escrow_account_id,c.pending_account_id,c.amount,'referral-daily-refund:'||r.id::text||':restore-pending'))
 AND (c.state<>'succeeded' OR referral_daily_journal_valid(c.release_remainder_journal_id,c.escrow_account_id,c.fees_account_id,c.amount-c.fee_contribution,'referral-daily-refund:'||r.id::text||':release-remainder'))
 AND EXISTS(SELECT 1 FROM referral_daily_events e WHERE e.order_id=c.order_id AND e.operation='refund-pending' AND e.reference_id=r.id AND e.subject_hash=c.request_hash AND e.actor_name='refund-engine')
 AND (c.state='pending' OR EXISTS(SELECT 1 FROM referral_daily_events e WHERE e.order_id=c.order_id AND e.operation='refund-'||c.state AND e.reference_id=r.id AND e.subject_hash=c.result_hash AND e.actor_name='refund-engine'))
 AND (c.previous_result_hash IS NULL OR EXISTS(SELECT 1 FROM referral_daily_events e WHERE e.order_id=c.order_id AND e.operation='refund-unknown' AND e.reference_id=r.id AND e.subject_hash=c.previous_result_hash AND e.actor_name='refund-engine')) AS valid
 FROM referral_daily_refund_reservations c JOIN refunds r ON r.id=c.refund_id JOIN referral_daily_liabilities l ON l.order_id=c.order_id JOIN referral_daily_terms d ON d.order_id=c.order_id
 JOIN referral_daily_liability_sources src ON src.order_id=c.order_id JOIN accounts es ON es.id=c.escrow_account_id JOIN accounts fr ON fr.id=c.fee_reserved_account_id LEFT JOIN journals fj ON fj.event_key='refund-fee:'||r.id::text||':reserve';
CREATE FUNCTION guard_referral_daily_refund_source() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_daily_refund_invariants WHERE refund_id=NEW.refund_id AND valid IS TRUE) THEN RAISE EXCEPTION 'daily refund original funding or audit invalid';END IF;RETURN NULL;END $$;
CREATE CONSTRAINT TRIGGER referral_daily_refund_source AFTER INSERT OR UPDATE ON referral_daily_refund_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_referral_daily_refund_source();
CREATE OR REPLACE FUNCTION guard_referral_daily_award() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_daily_award_invariants WHERE order_id=NEW.order_id AND valid IS TRUE) OR EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=NEW.order_id AND c.state<>'failed') THEN RAISE EXCEPTION 'daily award lacks funded source or has refund obligation';END IF;RETURN NULL;END $$;
-- Only this complete, funded family can explain a paid positive v1 term
-- without an issued reward. Application HMAC verification remains mandatory.
CREATE VIEW referral_daily_liability_invariants AS SELECT l.order_id,
 src.valid IS TRUE
 AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_invariants v WHERE v.refund_id=c.refund_id AND v.valid IS TRUE))
 AND NOT (EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND c.state='succeeded') AND EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND c.state IN('pending','unknown')))
 AND CASE WHEN EXISTS(SELECT 1 FROM referral_daily_awards a WHERE a.order_id=l.order_id) THEN
 EXISTS(SELECT 1 FROM referral_daily_award_invariants a WHERE a.order_id=l.order_id AND a.valid IS TRUE)
 AND p.balance=0 AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND c.state<>'failed')
 ELSE NOT EXISTS(SELECT 1 FROM referral_rewards r WHERE r.order_id=l.order_id)
 AND p.balance=CASE WHEN EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND c.state<>'failed') THEN 0 ELSE d.reward_amount END
 AND NOT EXISTS(SELECT 1 FROM refunds r WHERE r.order_id=l.order_id AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.refund_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.order_id=l.order_id AND c.state='succeeded' AND c.refund_id<r.id)) END AS valid
 FROM referral_daily_liabilities l JOIN referral_daily_terms d ON d.order_id=l.order_id JOIN accounts p ON p.id=d.pending_account_id JOIN referral_daily_liability_sources src ON src.order_id=l.order_id;
-- Keep the large structural projection behind a non-inlined, per-row boundary.
-- Small integrity reads must not spend seconds JIT-compiling the whole family,
-- particularly when the installation has no deferred source rows at all.
CREATE FUNCTION referral_daily_liability_valid(oid bigint) RETURNS boolean LANGUAGE SQL STABLE SET jit=off AS $$ SELECT EXISTS(SELECT 1 FROM referral_daily_liability_invariants WHERE order_id=oid AND valid IS TRUE) $$;
CREATE FUNCTION referral_daily_refund_valid(rid bigint) RETURNS boolean LANGUAGE SQL STABLE SET jit=off AS $$ SELECT EXISTS(SELECT 1 FROM referral_daily_refund_invariants WHERE refund_id=rid AND valid IS TRUE) $$;
CREATE FUNCTION referral_daily_award_valid(oid bigint) RETURNS boolean LANGUAGE SQL STABLE SET jit=off AS $$ SELECT EXISTS(SELECT 1 FROM referral_daily_award_invariants WHERE order_id=oid AND valid IS TRUE) $$;
