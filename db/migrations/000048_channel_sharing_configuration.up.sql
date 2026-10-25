CREATE TABLE channel_sharing_receivers (
 id bigserial PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),channel_id bigint NOT NULL CHECK(channel_id>0),
 external_no text NOT NULL CHECK(external_no ~ '^[A-Za-z0-9_-]{1,128}$'),
 provider text NOT NULL CHECK(provider IN ('wxpay','alipay')),app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 product text NOT NULL CHECK(product IN ('wxpay_v3_direct','alipay_direct')),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),input_hash bytea NOT NULL CHECK(octet_length(input_hash)=32),
 relation_account_hash bytea NOT NULL CHECK(octet_length(relation_account_hash)=32),relation_hash bytea NOT NULL UNIQUE CHECK(octet_length(relation_hash)=32),
 relation_auth_hash bytea NOT NULL UNIQUE CHECK(octet_length(relation_auth_hash)=32),
 receiver_cipher bytea NOT NULL CHECK(octet_length(receiver_cipher) BETWEEN 28 AND 16384),
 state text NOT NULL DEFAULT 'draft' CHECK(state IN ('draft','binding','bound','unbinding','unbound','unknown')),
 last_operation_id bigint,created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 UNIQUE(merchant_id,external_no),
 CHECK(provider='wxpay' AND product='wxpay_v3_direct' OR provider='alipay' AND product='alipay_direct')
);
CREATE TABLE channel_sharing_receiver_operations (
 id bigserial PRIMARY KEY,receiver_id bigint NOT NULL REFERENCES channel_sharing_receivers(id),
 action text NOT NULL CHECK(action IN ('bind','unbind')),out_no text NOT NULL UNIQUE,
 external_no text NOT NULL CHECK(external_no ~ '^[A-Za-z0-9_-]{1,128}$'),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 state text NOT NULL DEFAULT 'unsent' CHECK(state IN ('unsent','claimed','succeeded','failed','unknown')),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,
 actor_class text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT statement_timestamp(),claimed_at timestamptz,
 CHECK(out_no ~ '^P[BU][a-f0-9]{30}$' AND left(out_no,2)=CASE WHEN action='bind' THEN 'PB' ELSE 'PU' END),
 CHECK((state='unsent')=(claimed_at IS NULL)),UNIQUE(receiver_id,external_no)
);
ALTER TABLE channel_sharing_receivers ADD CONSTRAINT channel_sharing_receiver_last_operation FOREIGN KEY(last_operation_id) REFERENCES channel_sharing_receiver_operations(id);
CREATE INDEX channel_sharing_receiver_owner ON channel_sharing_receivers(merchant_id,channel_id,id);
CREATE FUNCTION channel_sharing_receiver_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE op channel_sharing_receiver_operations%ROWTYPE;
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.state<>'draft' OR NEW.last_operation_id IS NOT NULL THEN RAISE EXCEPTION 'new sharing receiver must be draft'; END IF; RETURN NEW; END IF;
 IF (NEW.id,NEW.merchant_id,NEW.channel_id,NEW.external_no,NEW.provider,NEW.app_id,NEW.product,NEW.account_hash,NEW.input_hash,NEW.relation_account_hash,NEW.relation_hash,NEW.relation_auth_hash,NEW.receiver_cipher,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.channel_id,OLD.external_no,OLD.provider,OLD.app_id,OLD.product,OLD.account_hash,OLD.input_hash,OLD.relation_account_hash,OLD.relation_hash,OLD.relation_auth_hash,OLD.receiver_cipher,OLD.created_at)
 THEN RAISE EXCEPTION 'immutable sharing receiver identity'; END IF;
 IF NEW.state=OLD.state THEN IF NEW.last_operation_id IS DISTINCT FROM OLD.last_operation_id THEN RAISE EXCEPTION 'sharing operation cannot be rebound'; END IF; RETURN NEW; END IF;
 SELECT * INTO op FROM channel_sharing_receiver_operations WHERE id=NEW.last_operation_id;
 IF op.id IS NULL OR op.receiver_id<>OLD.id THEN RAISE EXCEPTION 'missing original receiver operation'; END IF;
 IF OLD.state IN ('draft','unbound') AND NEW.state='binding' AND op.action='bind' AND op.state='claimed' THEN RETURN NEW; END IF;
 IF OLD.state='bound' AND NEW.state='unbinding' AND op.action='unbind' AND op.state='claimed' THEN RETURN NEW; END IF;
 IF NEW.last_operation_id IS DISTINCT FROM OLD.last_operation_id THEN RAISE EXCEPTION 'sharing result changed original operation'; END IF;
 IF OLD.state='binding' AND op.action='bind' AND ((NEW.state='bound' AND op.state='succeeded') OR (NEW.state='draft' AND op.state='failed') OR (NEW.state='unknown' AND op.state='unknown')) THEN RETURN NEW; END IF;
 IF OLD.state='unbinding' AND op.action='unbind' AND ((NEW.state='unbound' AND op.state='succeeded') OR (NEW.state='bound' AND op.state='failed') OR (NEW.state='unknown' AND op.state='unknown')) THEN RETURN NEW; END IF;
 IF OLD.state='unknown' AND ((op.action='bind' AND NEW.state='bound') OR (op.action='unbind' AND NEW.state='unbound')) AND op.state='succeeded' THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'invalid sharing receiver transition';
END $$;
CREATE TRIGGER channel_sharing_receiver_binding BEFORE INSERT OR UPDATE ON channel_sharing_receivers FOR EACH ROW EXECUTE FUNCTION channel_sharing_receiver_guard();
CREATE TRIGGER channel_sharing_receiver_no_delete BEFORE DELETE ON channel_sharing_receivers FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_sharing_receiver_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.state<>'unsent' THEN RAISE EXCEPTION 'new receiver operation must be unsent'; END IF; RETURN NEW; END IF;
 IF (NEW.id,NEW.receiver_id,NEW.action,NEW.out_no,NEW.external_no,NEW.request_hash,NEW.actor_role,NEW.actor_id,NEW.actor_name,NEW.actor_class,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.receiver_id,OLD.action,OLD.out_no,OLD.external_no,OLD.request_hash,OLD.actor_role,OLD.actor_id,OLD.actor_name,OLD.actor_class,OLD.created_at)
 OR OLD.claimed_at IS NOT NULL AND NEW.claimed_at IS DISTINCT FROM OLD.claimed_at
 THEN RAISE EXCEPTION 'immutable receiver operation'; END IF;
 IF NEW.state=OLD.state THEN RETURN NEW; END IF;
 IF OLD.state='unsent' AND NEW.state='claimed' OR OLD.state='claimed' AND NEW.state IN ('succeeded','failed','unknown') OR OLD.state='unknown' AND NEW.state IN ('succeeded','failed') THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'receiver operation cannot resend';
END $$;
CREATE TRIGGER channel_sharing_receiver_operation_binding BEFORE INSERT OR UPDATE ON channel_sharing_receiver_operations FOR EACH ROW EXECUTE FUNCTION channel_sharing_receiver_operation_guard();
CREATE TRIGGER channel_sharing_receiver_operation_no_delete BEFORE DELETE ON channel_sharing_receiver_operations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE channel_sharing_rules (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),channel_id bigint NOT NULL CHECK(channel_id>0),
 revision bigint NOT NULL CHECK(revision>0),provider text NOT NULL,app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),product text NOT NULL,
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),input_hash bytea NOT NULL CHECK(octet_length(input_hash)=32),
 enabled boolean NOT NULL,minimum_amount bigint NOT NULL CHECK(minimum_amount BETWEEN 0 AND 999999999999),
 methods jsonb NOT NULL CHECK(jsonb_typeof(methods)='array' AND jsonb_array_length(methods) BETWEEN 1 AND 10),
 refund_mode text NOT NULL CHECK(refund_mode='merchant_funded'),auto_finish boolean NOT NULL CHECK(auto_finish),
 receiver_count integer NOT NULL CHECK(receiver_count BETWEEN 1 AND 50),created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 UNIQUE(merchant_id,channel_id,revision),CHECK(provider='wxpay' AND product='wxpay_v3_direct' OR provider='alipay' AND product='alipay_direct')
);
CREATE TABLE channel_sharing_rule_items (
 rule_id bigint NOT NULL REFERENCES channel_sharing_rules(id),position integer NOT NULL CHECK(position BETWEEN 1 AND 50),
 receiver_id bigint NOT NULL REFERENCES channel_sharing_receivers(id),mode text NOT NULL,
 rate_bps bigint NOT NULL DEFAULT 0,fixed_amount bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(rule_id,position),UNIQUE(rule_id,receiver_id),
 CHECK(mode='rate' AND rate_bps BETWEEN 1 AND 10000 AND fixed_amount=0 OR mode='fixed' AND fixed_amount BETWEEN 1 AND 999999999999 AND rate_bps=0)
);
CREATE TABLE channel_sharing_rule_current (
 merchant_id bigint NOT NULL REFERENCES merchants(id),channel_id bigint NOT NULL CHECK(channel_id>0),rule_id bigint NOT NULL UNIQUE REFERENCES channel_sharing_rules(id),
 PRIMARY KEY(merchant_id,channel_id)
);
CREATE TRIGGER channel_sharing_rule_immutable BEFORE UPDATE OR DELETE ON channel_sharing_rules FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_rule_item_immutable BEFORE UPDATE OR DELETE ON channel_sharing_rule_items FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_rule_current_no_delete BEFORE DELETE ON channel_sharing_rule_current FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_sharing_rule_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint; r channel_sharing_rules%ROWTYPE; cnt integer; last_position integer; rates bigint; fixed bigint;
BEGIN
 IF TG_TABLE_NAME='channel_sharing_rules' THEN rid:=NEW.id; ELSE rid:=NEW.rule_id; END IF;
 SELECT * INTO STRICT r FROM channel_sharing_rules WHERE id=rid;
 SELECT count(*),max(position),COALESCE(sum(rate_bps),0),COALESCE(sum(fixed_amount),0) INTO cnt,last_position,rates,fixed FROM channel_sharing_rule_items WHERE rule_id=rid;
 IF cnt<>r.receiver_count OR last_position<>cnt OR rates>10000 OR fixed>999999999999 THEN RAISE EXCEPTION 'incomplete sharing rule'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_rule_items i JOIN channel_sharing_receivers a ON a.id=i.receiver_id WHERE i.rule_id=rid AND
  ((a.merchant_id,a.channel_id,a.provider,a.app_id,a.product,a.account_hash) IS DISTINCT FROM (r.merchant_id,r.channel_id,r.provider,r.app_id,r.product,r.account_hash) OR r.enabled AND a.state<>'bound'))
 THEN RAISE EXCEPTION 'sharing rule receiver scope or state mismatch'; END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(r.methods) m WHERE jsonb_typeof(m)<>'string' OR trim(both '"' FROM m::text) !~ '^[a-z][a-z0-9_]{0,31}$') OR
 (SELECT count(DISTINCT m) FROM jsonb_array_elements(r.methods) m)<>jsonb_array_length(r.methods) THEN RAISE EXCEPTION 'invalid sharing rule methods'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER channel_sharing_rule_complete_parent AFTER INSERT ON channel_sharing_rules DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_rule_complete();
CREATE CONSTRAINT TRIGGER channel_sharing_rule_complete_items AFTER INSERT ON channel_sharing_rule_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_rule_complete();
CREATE FUNCTION channel_sharing_rule_current_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r channel_sharing_rules%ROWTYPE; previous_revision bigint;
BEGIN
 SELECT * INTO STRICT r FROM channel_sharing_rules WHERE id=NEW.rule_id;
 IF (r.merchant_id,r.channel_id) IS DISTINCT FROM (NEW.merchant_id,NEW.channel_id) THEN RAISE EXCEPTION 'sharing rule pointer scope mismatch'; END IF;
 IF TG_OP='INSERT' THEN IF r.revision<>1 THEN RAISE EXCEPTION 'first sharing rule revision must be one'; END IF;
 ELSE
  IF (NEW.merchant_id,NEW.channel_id) IS DISTINCT FROM (OLD.merchant_id,OLD.channel_id) THEN RAISE EXCEPTION 'immutable sharing rule pointer owner'; END IF;
  IF NEW.rule_id=OLD.rule_id THEN RETURN NEW; END IF;
  SELECT revision INTO STRICT previous_revision FROM channel_sharing_rules WHERE id=OLD.rule_id;
  IF r.revision<>previous_revision+1 THEN RAISE EXCEPTION 'sharing rule pointer must advance one revision'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_rule_current_binding BEFORE INSERT OR UPDATE ON channel_sharing_rule_current FOR EACH ROW EXECUTE FUNCTION channel_sharing_rule_current_guard();
CREATE FUNCTION channel_sharing_active_rule_receiver_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='bound' AND NEW.state='unbinding' AND EXISTS(SELECT 1 FROM channel_sharing_rule_items i JOIN channel_sharing_rules r ON r.id=i.rule_id JOIN channel_sharing_rule_current c ON c.rule_id=r.id WHERE i.receiver_id=OLD.id AND r.enabled)
 THEN RAISE EXCEPTION 'receiver is used by an enabled sharing rule'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_active_rule_receiver BEFORE UPDATE ON channel_sharing_receivers FOR EACH ROW EXECUTE FUNCTION channel_sharing_active_rule_receiver_guard();
