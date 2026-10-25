-- Configured estimated cost is not a billed expense or a new ledger source.
-- Existing terms and all v1/v2 proof formats remain unchanged.
-- The surviving order identifies new application births even if all child
-- evidence is lost. Version 0 preserves existing pre-71 orders without backfill.
ALTER TABLE orders ADD COLUMN referral_source_version integer NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN referral_source_hash bytea;
ALTER TABLE orders ADD CONSTRAINT orders_referral_source_shape CHECK
 ((referral_source_version=0 AND referral_source_hash IS NULL) OR
  (referral_source_version=3 AND referral_source_hash IS NOT NULL AND octet_length(referral_source_hash)=32));
CREATE FUNCTION referral_order_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.referral_source_version,NEW.referral_source_hash) IS NOT DISTINCT FROM
 (OLD.referral_source_version,OLD.referral_source_hash) THEN RETURN NEW; END IF;
 IF OLD.referral_source_version<>0 OR OLD.referral_source_hash IS NOT NULL
 OR NEW.referral_source_version<>3 OR NEW.referral_source_hash IS NULL
 OR OLD.status<>'created' OR OLD.created_at<>transaction_timestamp()
 OR NOT EXISTS(SELECT 1 FROM referral_order_terms t WHERE t.order_id=OLD.id)
 THEN RAISE EXCEPTION 'original order referral source is immutable and birth-only'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER orders_referral_source_immutable BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION referral_order_source_guard();
ALTER TABLE referral_policies DROP CONSTRAINT referral_policies_basis_check;
ALTER TABLE referral_policies ADD CONSTRAINT referral_policies_basis_check CHECK(basis IN('fee','gross','profit'));
ALTER TABLE referral_daily_terms DROP CONSTRAINT referral_daily_terms_basis_check;
ALTER TABLE referral_daily_terms ADD CONSTRAINT referral_daily_terms_basis_check CHECK(basis IN('fee','gross','profit'));
CREATE TABLE referral_profit_costs (
 order_id bigint PRIMARY KEY REFERENCES referral_order_terms(order_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 source_version integer NOT NULL CHECK(source_version=3),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 method text NOT NULL CHECK(method ~ '^[a-z][a-z0-9_]{0,31}$'),
 provider text NOT NULL CHECK(provider IN('alipay','wxpay')),
 app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 256),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 config_revision text NOT NULL CHECK(config_revision ~ '^[0-9a-f]{64}$'),
 rate_units bigint NOT NULL CHECK(rate_units BETWEEN 0 AND 100000000),
 amount bigint NOT NULL CHECK(amount>0),
 platform_fee bigint NOT NULL CHECK(platform_fee BETWEEN 0 AND amount),
 cost_whole bigint NOT NULL CHECK(cost_whole BETWEEN 0 AND amount),
 cost_remainder bigint NOT NULL CHECK(cost_remainder BETWEEN 0 AND 99999999),
 profit_amount bigint NOT NULL CHECK(profit_amount BETWEEN 0 AND platform_fee),
 reward_amount bigint NOT NULL CHECK(reward_amount BETWEEN 0 AND profit_amount),
 terms_hash bytea NOT NULL CHECK(octet_length(terms_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 CHECK(cost_whole = (amount/100000000)*rate_units + ((amount%100000000)*rate_units)/100000000),
 CHECK(cost_remainder = ((amount%100000000)*rate_units)%100000000),
 CHECK(profit_amount=greatest(platform_fee-cost_whole-CASE WHEN cost_remainder>50000000 THEN 1 ELSE 0 END,0))
);
CREATE TRIGGER referral_profit_cost_immutable BEFORE UPDATE OR DELETE ON referral_profit_costs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE referral_profit_cost_events (
 order_id bigint PRIMARY KEY REFERENCES referral_profit_costs(order_id),
 event_key text NOT NULL UNIQUE,
 actor text NOT NULL CHECK(actor='payment-engine'),
 subject_hash bytea NOT NULL CHECK(octet_length(subject_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER referral_profit_event_immutable BEFORE UPDATE OR DELETE ON referral_profit_cost_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION referral_profit_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o orders; t referral_order_terms;
BEGIN
 SELECT * INTO STRICT o FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO STRICT t FROM referral_order_terms WHERE order_id=o.id;
 IF o.status<>'created' OR o.created_at<>transaction_timestamp() OR t.purpose<>'payment'
 OR (o.merchant_id,o.type,o.amount,o.platform_fee,t.reward_amount,t.proof_hash) IS DISTINCT FROM
 (NEW.merchant_id,NEW.provider,NEW.amount,NEW.platform_fee,NEW.reward_amount,NEW.terms_hash)
 THEN RAISE EXCEPTION 'profit cost must be born with exact original terms'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_profit_birth BEFORE INSERT ON referral_profit_costs FOR EACH ROW EXECUTE FUNCTION referral_profit_birth_guard();
CREATE FUNCTION referral_profit_audit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_profit_cost_events e WHERE e.order_id=NEW.order_id
 AND e.event_key='referral-profit:'||NEW.order_id::text||':birth' AND e.subject_hash=NEW.proof_hash AND e.actor='payment-engine')
 THEN RAISE EXCEPTION 'missing original profit cost audit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_profit_requires_audit AFTER INSERT ON referral_profit_costs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_profit_audit_guard();

CREATE FUNCTION referral_profit_required(oid bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT t.purpose='payment' AND t.relation_id IS NOT NULL
 AND t.reason NOT IN('disabled','promoter_disabled') AND
 (SELECT p.basis FROM jsonb_array_elements_text(t.policy_versions) WITH ORDINALITY r(v,n)
 JOIN referral_policies p ON p.id=r.v::bigint WHERE r.n<=3 ORDER BY r.n DESC LIMIT 1)='profit'
 FROM referral_order_terms t WHERE t.order_id=oid),false)
$$;
CREATE FUNCTION referral_profit_terms_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c referral_profit_costs; t referral_order_terms; required boolean;
BEGIN
 SELECT * INTO STRICT t FROM referral_order_terms WHERE order_id=NEW.order_id;
 required := referral_profit_required(NEW.order_id);
 IF required AND NOT EXISTS(SELECT 1 FROM orders o WHERE o.id=NEW.order_id AND o.referral_source_version=3 AND octet_length(o.referral_source_hash)=32)
 THEN RAISE EXCEPTION 'profit cost requires original order source anchor'; END IF;
 SELECT * INTO c FROM referral_profit_costs WHERE order_id=t.order_id;
 IF required IS DISTINCT FROM FOUND THEN RAISE EXCEPTION 'original profit cost family mismatch'; END IF;
 IF required AND (c.reward_amount IS DISTINCT FROM
 ((c.profit_amount/10000)*t.rate_bps + ((c.profit_amount%10000)*t.rate_bps+5000)/10000))
 THEN RAISE EXCEPTION 'original profit reward mismatch'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_profit_terms_require_cost AFTER INSERT ON referral_order_terms DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_profit_terms_guard();
CREATE CONSTRAINT TRIGGER referral_profit_cost_requires_terms AFTER INSERT ON referral_profit_costs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_profit_terms_guard();
CREATE FUNCTION referral_profit_event_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_profit_costs c WHERE c.order_id=NEW.order_id AND c.created_at=transaction_timestamp()
 AND c.proof_hash=NEW.subject_hash AND NEW.event_key='referral-profit:'||c.order_id::text||':birth')
 THEN RAISE EXCEPTION 'original profit event mismatch'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_profit_event_consistency BEFORE INSERT ON referral_profit_cost_events FOR EACH ROW EXECUTE FUNCTION referral_profit_event_guard();
