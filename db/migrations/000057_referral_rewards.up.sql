-- Explicit ordinary-payment fee rewards. This is deliberately not an import
-- of unproven encoded Pro reward, membership, profit or daily-task semantics.
CREATE TABLE referral_policies (
 id bigserial PRIMARY KEY,
 scope_kind text NOT NULL CHECK(scope_kind IN ('global','group')),
 scope_id bigint NOT NULL CHECK((scope_kind='global' AND scope_id=0) OR (scope_kind='group' AND scope_id>0)),
 revision bigint NOT NULL CHECK(revision>0),
 enabled boolean,
 rate_bps integer CHECK(rate_bps BETWEEN 0 AND 10000),
 mode text NOT NULL CHECK(mode='immediate'),
 basis text NOT NULL CHECK(basis='fee'),
 refund_policy text NOT NULL CHECK(refund_policy='any_successful_refund_voids_reward'),
 actor_role text NOT NULL CHECK(actor_role='admin'),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 128),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(scope_kind,scope_id,revision),
 CHECK(scope_kind<>'global' OR (enabled IS NOT NULL AND rate_bps IS NOT NULL))
);
CREATE TRIGGER referral_policy_immutable BEFORE UPDATE OR DELETE ON referral_policies FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE referral_policy_heads (
 scope_kind text NOT NULL,
 scope_id bigint NOT NULL,
 policy_id bigint NOT NULL UNIQUE REFERENCES referral_policies(id),
 PRIMARY KEY(scope_kind,scope_id)
);
CREATE FUNCTION referral_policy_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p referral_policies; previous_revision bigint;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'referral policy heads cannot be removed'; END IF;
 SELECT * INTO STRICT p FROM referral_policies WHERE id=NEW.policy_id;
 IF (p.scope_kind,p.scope_id) IS DISTINCT FROM (NEW.scope_kind,NEW.scope_id) THEN RAISE EXCEPTION 'referral policy scope mismatch'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (OLD.scope_kind,OLD.scope_id) IS DISTINCT FROM (NEW.scope_kind,NEW.scope_id) THEN RAISE EXCEPTION 'immutable referral policy scope'; END IF;
  SELECT revision INTO STRICT previous_revision FROM referral_policies WHERE id=OLD.policy_id;
  IF p.revision<>previous_revision+1 THEN RAISE EXCEPTION 'referral policy revision conflict'; END IF;
 ELSE
  IF p.revision<>1 THEN RAISE EXCEPTION 'referral policy initial revision conflict'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_policy_head_consistency BEFORE INSERT OR UPDATE OR DELETE ON referral_policy_heads FOR EACH ROW EXECUTE FUNCTION referral_policy_head_guard();
CREATE TABLE referral_policy_audits (
 policy_id bigint PRIMARY KEY REFERENCES referral_policies(id),
 actor_role text NOT NULL,
 actor_id bigint NOT NULL,
 actor_name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER referral_policy_audit_immutable BEFORE UPDATE OR DELETE ON referral_policy_audits FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION referral_policy_audit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p referral_policies;
BEGIN
 SELECT * INTO STRICT p FROM referral_policies WHERE id=NEW.id;
 IF NOT EXISTS(SELECT 1 FROM referral_policy_audits a WHERE a.policy_id=p.id AND (a.actor_role,a.actor_id,a.actor_name)=(p.actor_role,p.actor_id,p.actor_name)) THEN RAISE EXCEPTION 'missing referral policy audit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_policy_requires_audit AFTER INSERT ON referral_policies DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_policy_audit_guard();

CREATE TABLE referral_order_terms (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 relation_id bigint REFERENCES referral_relations(id),
 promoter_id bigint REFERENCES merchants(id),
 purpose text NOT NULL CHECK(purpose IN ('payment','recharge')),
 enabled boolean NOT NULL,
 reason text NOT NULL CHECK(reason IN ('eligible','disabled','no_relation','promoter_disabled','zero_fee','zero_rate','recharge')),
 rate_bps integer NOT NULL CHECK(rate_bps BETWEEN 0 AND 10000),
 platform_fee bigint NOT NULL CHECK(platform_fee>=0),
 reward_amount bigint NOT NULL CHECK(reward_amount>=0 AND reward_amount<=platform_fee),
 refund_policy text NOT NULL CHECK(refund_policy='any_successful_refund_voids_reward'),
 policy_versions jsonb NOT NULL CHECK(jsonb_typeof(policy_versions)='array' AND jsonb_array_length(policy_versions)=5),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 CHECK((relation_id IS NULL)=(promoter_id IS NULL)),
 CHECK(promoter_id IS NULL OR promoter_id<>merchant_id),
 CHECK(enabled=(reward_amount>0)),
 CHECK((reason='eligible')=enabled),
 CHECK(NOT enabled OR (purpose='payment' AND promoter_id IS NOT NULL AND rate_bps>0)),
 CHECK(purpose<>'recharge' OR (NOT enabled AND reason='recharge'))
);
CREATE TRIGGER referral_terms_immutable BEFORE UPDATE OR DELETE ON referral_order_terms FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION referral_terms_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o orders;
BEGIN
 SELECT * INTO STRICT o FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF o.status<>'created' OR o.created_at<>transaction_timestamp() OR o.merchant_id<>NEW.merchant_id OR o.platform_fee<>NEW.platform_fee THEN RAISE EXCEPTION 'referral terms must be born with the original order'; END IF;
 IF (NEW.purpose='recharge') IS DISTINCT FROM EXISTS(SELECT 1 FROM recharge_intents i WHERE i.order_id=o.id) THEN RAISE EXCEPTION 'referral payment purpose mismatch'; END IF;
 IF NEW.relation_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM referral_relations r WHERE r.id=NEW.relation_id AND r.invited_id=o.merchant_id AND r.promoter_id=NEW.promoter_id) THEN RAISE EXCEPTION 'referral relation mismatch'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_terms_birth BEFORE INSERT ON referral_order_terms FOR EACH ROW EXECUTE FUNCTION referral_terms_birth_guard();
CREATE FUNCTION referral_recharge_purpose_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM referral_order_terms t WHERE t.order_id=NEW.order_id AND t.purpose<>'recharge') THEN RAISE EXCEPTION 'original referral purpose is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_recharge_purpose BEFORE INSERT ON recharge_intents FOR EACH ROW EXECUTE FUNCTION referral_recharge_purpose_guard();

CREATE TABLE referral_rewards (
 id bigserial PRIMARY KEY,
 order_id bigint NOT NULL UNIQUE REFERENCES referral_order_terms(order_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 promoter_id bigint NOT NULL REFERENCES merchants(id),
 amount bigint NOT NULL CHECK(amount>0),
 terms_hash bytea NOT NULL CHECK(octet_length(terms_hash)=32),
 journal_id bigint NOT NULL UNIQUE REFERENCES journals(id),
 channel_id bigint NOT NULL,
 provider_trade_no text NOT NULL CHECK(length(provider_trade_no) BETWEEN 1 AND 256),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 CHECK(merchant_id<>promoter_id)
);
CREATE TRIGGER referral_reward_immutable BEFORE UPDATE OR DELETE ON referral_rewards FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE referral_reward_audits (
 reward_id bigint PRIMARY KEY REFERENCES referral_rewards(id),
 event_key text NOT NULL UNIQUE,
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER referral_reward_audit_immutable BEFORE UPDATE OR DELETE ON referral_reward_audits FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE VIEW referral_reward_invariants AS
SELECT r.id AS reward_id,
 o.status='paid' AND t.enabled AND t.purpose='payment' AND t.reason='eligible'
 AND (r.merchant_id,r.promoter_id,r.amount,r.terms_hash)=(t.merchant_id,t.promoter_id,t.reward_amount,t.proof_hash)
 AND o.merchant_id=t.merchant_id AND o.platform_fee=t.platform_fee AND r.amount<=o.platform_fee
 AND NOT EXISTS(SELECT 1 FROM recharge_intents i WHERE i.order_id=o.id)
 AND EXISTS(SELECT 1 FROM verified_payment_receipts v WHERE v.order_id=o.id AND v.channel_id=r.channel_id AND v.provider_trade_no=r.provider_trade_no)
 AND j.event_key='referral-award:'||o.id::text
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-r.amount FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=j.id AND a.owner_id=0 AND a.kind='fees' AND a.scope_id=0)
 AND (SELECT COALESCE(sum(e.amount),0)=r.amount FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=j.id AND a.owner_id=r.promoter_id AND a.kind='available' AND a.scope_id=0)
 AND EXISTS(SELECT 1 FROM journals fj WHERE fj.event_key='payment-fee:'||o.trade_no
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=fj.id)
  AND (SELECT COALESCE(sum(e.amount),0)=o.platform_fee FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=fj.id AND a.owner_id=0 AND a.kind='fees' AND a.scope_id=0)
  AND (SELECT COALESCE(sum(e.amount),0)=-o.platform_fee FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=fj.id AND a.owner_id=0 AND a.kind='clearing' AND a.scope_id=0))
 AND EXISTS(SELECT 1 FROM referral_reward_audits a WHERE a.reward_id=r.id AND a.event_key='referral-award:'||o.id::text)
 AS valid
FROM referral_rewards r JOIN referral_order_terms t ON t.order_id=r.order_id JOIN orders o ON o.id=r.order_id JOIN journals j ON j.id=r.journal_id;
CREATE FUNCTION referral_reward_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_reward_invariants v WHERE v.reward_id=NEW.id AND v.valid IS TRUE) THEN RAISE EXCEPTION 'invalid original fee reward'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_reward_requires_proof AFTER INSERT ON referral_rewards DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_reward_guard();
