-- New balance membership purchases use only their own isolated income.
ALTER TABLE membership_purchases ADD COLUMN source_version integer NOT NULL DEFAULT 0 CHECK(source_version IN(0,1));
CREATE FUNCTION membership_referral_legacy_snapshot(p membership_purchases) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('id',p.id,'merchant_id',p.merchant_id,'group_id',p.group_id,'external_no',p.external_no,'price',p.price,'group_name',p.group_name,'duration_days',p.duration_days,'actor',p.actor,'source_version',p.source_version,
 'expires_at',to_char(p.expires_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'created_at',to_char(p.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) $$;
CREATE TABLE membership_referral_legacy_purchases(
 purchase_id bigint PRIMARY KEY REFERENCES membership_purchases(id),
 row_snapshot jsonb NOT NULL CHECK(jsonb_typeof(row_snapshot)='object'),
 row_hash bytea NOT NULL CHECK(octet_length(row_hash)=32 AND row_hash=sha256(convert_to(row_snapshot::text,'UTF8')))
);
INSERT INTO membership_referral_legacy_purchases SELECT id,membership_referral_legacy_snapshot(p),sha256(convert_to(membership_referral_legacy_snapshot(p)::text,'UTF8')) FROM membership_purchases p;
CREATE TRIGGER membership_referral_legacy_closed BEFORE INSERT OR UPDATE OR DELETE ON membership_referral_legacy_purchases FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE membership_purchases ALTER COLUMN source_version SET DEFAULT 1;
CREATE FUNCTION guard_membership_purchase_version() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.source_version IS DISTINCT FROM 1 THEN RAISE EXCEPTION 'new membership purchase requires source version one';END IF;RETURN NEW;END $$;
CREATE TRIGGER membership_purchase_version BEFORE INSERT ON membership_purchases FOR EACH ROW EXECUTE FUNCTION guard_membership_purchase_version();
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold','settlement_reserve','channel_sharing_reserve','referral_clawback_reserve','order_income_reserve','order_income_refund_reserve','referral_daily_pending','referral_daily_refund_reserve','membership_income'));
ALTER TABLE accounts DROP CONSTRAINT accounts_scope_domain;
ALTER TABLE accounts ADD CONSTRAINT accounts_scope_domain CHECK(
 kind IN('settlement_reserve','channel_sharing_reserve','order_income_reserve','order_income_refund_reserve') AND owner_id>0 AND scope_id>0
 OR kind IN('referral_clawback_reserve','referral_daily_pending','referral_daily_refund_reserve','membership_income') AND owner_id=0 AND scope_id>0
 OR kind NOT IN('settlement_reserve','channel_sharing_reserve','referral_clawback_reserve','order_income_reserve','order_income_refund_reserve','referral_daily_pending','referral_daily_refund_reserve','membership_income') AND scope_id=0);
ALTER TABLE accounts ADD CONSTRAINT membership_income_nonnegative CHECK(kind<>'membership_income' OR balance>=0);
CREATE TABLE membership_referral_policies(
 id bigserial PRIMARY KEY,scope_kind text NOT NULL CHECK(scope_kind IN('global','group')),scope_id bigint NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),enabled boolean,rate_bps bigint CHECK(rate_bps BETWEEN 0 AND 10000),
 actor_role text NOT NULL CHECK(actor_role='admin'),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL CHECK(length(actor_name)>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(scope_kind='global' AND scope_id=0 AND enabled IS NOT NULL AND rate_bps IS NOT NULL OR scope_kind='group' AND scope_id>0),UNIQUE(scope_kind,scope_id,revision)
);
CREATE TABLE membership_referral_policy_heads(scope_kind text NOT NULL,scope_id bigint NOT NULL,policy_id bigint NOT NULL UNIQUE REFERENCES membership_referral_policies(id),PRIMARY KEY(scope_kind,scope_id));
CREATE TABLE membership_referral_policy_audits(policy_id bigint PRIMARY KEY REFERENCES membership_referral_policies(id),actor_role text NOT NULL CHECK(actor_role='admin'),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL CHECK(length(actor_name)>0));
CREATE TRIGGER membership_referral_policy_immutable BEFORE UPDATE OR DELETE ON membership_referral_policies FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER membership_referral_policy_audit_immutable BEFORE UPDATE OR DELETE ON membership_referral_policy_audits FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE membership_referral_sources(
 purchase_id bigint PRIMARY KEY REFERENCES membership_purchases(id),source_version integer NOT NULL CHECK(source_version=1),merchant_id bigint NOT NULL REFERENCES merchants(id),
 promoter_id bigint REFERENCES merchants(id),relation_id bigint REFERENCES referral_relations(id),relation_hash bytea,
 payer_group_id bigint NOT NULL CHECK(payer_group_id>=0),promoter_group_id bigint NOT NULL CHECK(promoter_group_id>=0),
 policy_ids jsonb NOT NULL CHECK(jsonb_typeof(policy_ids)='array' AND jsonb_array_length(policy_ids)=3),policy_revisions jsonb NOT NULL CHECK(jsonb_typeof(policy_revisions)='array' AND jsonb_array_length(policy_revisions)=3),policy_hashes jsonb NOT NULL CHECK(jsonb_typeof(policy_hashes)='array' AND jsonb_array_length(policy_hashes)=3),
 buyer_revision bigint NOT NULL CHECK(buyer_revision>0),promoter_revision bigint NOT NULL CHECK(promoter_revision>=0),promoter_enabled boolean NOT NULL,enabled boolean NOT NULL,rate_bps bigint NOT NULL CHECK(rate_bps BETWEEN 0 AND 10000),
 price bigint NOT NULL CHECK(price>=0),reward_amount bigint NOT NULL CHECK(reward_amount>=0 AND reward_amount<=price),reason text NOT NULL CHECK(reason IN('no_relation','promoter_disabled','disabled','zero_price','zero_rate','rounds_zero','eligible')),
 income_account_id bigint UNIQUE REFERENCES accounts(id),purchase_journal_id bigint UNIQUE REFERENCES journals(id),transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK(promoter_id IS NULL AND relation_id IS NULL AND relation_hash IS NULL AND promoter_group_id=0 AND promoter_revision=0 OR promoter_id IS NOT NULL AND promoter_id<>merchant_id AND relation_id IS NOT NULL AND relation_hash IS NOT NULL AND octet_length(relation_hash)=32 AND promoter_revision>0),
 CHECK(price=0 AND reward_amount=0 AND income_account_id IS NULL AND purchase_journal_id IS NULL OR price>0 AND income_account_id IS NOT NULL AND purchase_journal_id IS NOT NULL),
 CHECK(reward_amount=0 AND reason<>'eligible' OR reward_amount>0 AND reason='eligible' AND enabled AND promoter_id IS NOT NULL)
);
CREATE TRIGGER membership_referral_source_immutable BEFORE UPDATE OR DELETE ON membership_referral_sources FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_membership_referral_birth() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(SELECT 1 FROM membership_purchases p WHERE p.id=NEW.purchase_id AND p.source_version=1 AND p.created_at=transaction_timestamp() AND (p.merchant_id,p.price)=(NEW.merchant_id,NEW.price)) THEN RAISE EXCEPTION 'membership referral requires original purchase transaction';END IF;
 IF NEW.price>0 AND NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=NEW.income_account_id AND (a.owner_id,a.kind,a.scope_id,a.balance)=(0::bigint,'membership_income',NEW.purchase_id,NEW.price-NEW.reward_amount)) THEN RAISE EXCEPTION 'membership referral requires exact isolated purchase income';END IF;RETURN NEW;END $$;
CREATE TRIGGER membership_referral_source_birth BEFORE INSERT ON membership_referral_sources FOR EACH ROW EXECUTE FUNCTION guard_membership_referral_birth();
CREATE TABLE membership_referral_rewards(
 purchase_id bigint PRIMARY KEY REFERENCES membership_referral_sources(purchase_id),promoter_id bigint NOT NULL REFERENCES merchants(id),amount bigint NOT NULL CHECK(amount>0),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),journal_id bigint NOT NULL UNIQUE REFERENCES journals(id),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER membership_referral_reward_immutable BEFORE UPDATE OR DELETE ON membership_referral_rewards FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE membership_referral_events(
 id bigserial PRIMARY KEY,purchase_id bigint NOT NULL REFERENCES membership_referral_sources(purchase_id),operation text NOT NULL CHECK(operation IN('source','award')),
 event_key text NOT NULL UNIQUE,actor text NOT NULL CHECK(length(actor)>0),subject_hash bytea NOT NULL CHECK(octet_length(subject_hash)=32),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(purchase_id,operation)
);
CREATE TRIGGER membership_referral_event_immutable BEFORE UPDATE OR DELETE ON membership_referral_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE VIEW membership_referral_source_invariants AS SELECT s.purchase_id,
 (p.source_version=1 AND s.source_version=1 AND (p.merchant_id,p.price)=(s.merchant_id,s.price)
 AND (s.price=0 AND s.income_account_id IS NULL AND s.purchase_journal_id IS NULL
 OR s.price>0 AND a.owner_id=0 AND a.kind='membership_income' AND a.scope_id=s.purchase_id AND a.balance=s.price-s.reward_amount
 AND j.event_key='membership:'||p.id::text AND j.transaction_id=s.transaction_id
 AND (SELECT count(*) FROM ledger_entries e WHERE e.journal_id=j.id)=2
 AND (SELECT count(*) FROM ledger_entries e JOIN accounts b ON b.id=e.account_id WHERE e.journal_id=j.id AND b.owner_id=p.merchant_id AND b.kind='available' AND b.scope_id=0 AND e.amount=-s.price)=1
 AND (SELECT count(*) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=a.id AND e.amount=s.price)=1)
 AND (s.reward_amount=0 AND r.purchase_id IS NULL OR s.reward_amount>0 AND r.purchase_id=s.purchase_id AND r.promoter_id=s.promoter_id AND r.amount=s.reward_amount AND r.source_hash=s.proof_hash
 AND x.event_key='membership-referral:'||p.id::text||':award' AND x.transaction_id=s.transaction_id AND (SELECT count(*) FROM ledger_entries e WHERE e.journal_id=x.id)=2
 AND (SELECT count(*) FROM ledger_entries e WHERE e.journal_id=x.id AND e.account_id=a.id AND e.amount=-s.reward_amount)=1
 AND (SELECT count(*) FROM ledger_entries e JOIN accounts b ON b.id=e.account_id WHERE e.journal_id=x.id AND b.owner_id=s.promoter_id AND b.kind='available' AND b.scope_id=0 AND e.amount=s.reward_amount)=1)) IS TRUE AS valid
 FROM membership_referral_sources s JOIN membership_purchases p ON p.id=s.purchase_id LEFT JOIN accounts a ON a.id=s.income_account_id LEFT JOIN journals j ON j.id=s.purchase_journal_id LEFT JOIN membership_referral_rewards r ON r.purchase_id=s.purchase_id LEFT JOIN journals x ON x.id=r.journal_id;
