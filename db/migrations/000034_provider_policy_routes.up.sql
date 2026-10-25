-- New executable rules require explicit scope/type; old descriptive records do not run.
CREATE UNIQUE INDEX pay_roll_active_scope ON backoffice_records((body->>'merchant_id'),(body->>'type'))
 WHERE resource='pay-rolls' AND body ? 'merchant_id' AND body ? 'type' AND body->'enabled'='true'::jsonb;
CREATE TABLE provider_route_counters (
 rule_id bigint PRIMARY KEY REFERENCES backoffice_records(id),
 cursor bigint NOT NULL DEFAULT 0 CHECK(cursor>=0),
 assigned bigint NOT NULL DEFAULT 0 CHECK(assigned>=0)
);
CREATE TABLE provider_route_assignments (
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 out_trade_no text NOT NULL CHECK(octet_length(out_trade_no) BETWEEN 1 AND 128),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 method text NOT NULL CHECK(method ~ '^[a-z][a-z0-9_]{0,31}$'),
 pay_type text NOT NULL CHECK(pay_type IN ('wxpay','alipay')),
 amount bigint NOT NULL CHECK(amount>0),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 rule_id bigint REFERENCES backoffice_records(id),
 rule_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(rule_snapshot)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(merchant_id,out_trade_no)
);
CREATE INDEX provider_route_rule_time ON provider_route_assignments(rule_id,created_at);
CREATE INDEX provider_route_channel_time ON provider_route_assignments(channel_id,created_at);
CREATE INDEX provider_attempt_channel_time ON provider_attempts(channel_id,created_at);
CREATE TRIGGER provider_route_immutable BEFORE UPDATE OR DELETE ON provider_route_assignments FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE OR REPLACE FUNCTION guard_provider_route_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM orders o JOIN provider_route_assignments r ON r.merchant_id=o.merchant_id AND r.out_trade_no=o.out_trade_no
           WHERE o.id=NEW.order_id AND (r.channel_id<>NEW.channel_id OR
             CASE WHEN r.method='applet' THEN CASE WHEN r.pay_type='wxpay' THEN 'jsapi' ELSE 'mini' END ELSE r.method END<>NEW.method)) THEN
  RAISE EXCEPTION 'provider attempt conflicts with immutable route';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_attempt_route BEFORE INSERT ON provider_attempts FOR EACH ROW EXECUTE FUNCTION guard_provider_route_attempt();
