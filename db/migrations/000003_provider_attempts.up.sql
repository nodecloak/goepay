-- One durable attempt per platform order. Unknown outcomes and crashed owners
-- cannot cause automatic resubmission on another provider or method.
CREATE TABLE provider_attempts (
 id bigserial PRIMARY KEY,
 order_id bigint NOT NULL UNIQUE REFERENCES orders(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 method text NOT NULL CHECK(method ~ '^[a-z][a-z0-9_]{0,31}$'),
 state text NOT NULL DEFAULT 'processing' CHECK(state IN ('processing','pending','unknown','failed')),
 material_type text NOT NULL DEFAULT '',
 material text NOT NULL DEFAULT '' CHECK(octet_length(material)<=1048576),
 provider_trade_no text NOT NULL DEFAULT '' CHECK(octet_length(provider_trade_no)<=256),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION guard_provider_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.order_id<>OLD.order_id OR NEW.channel_id<>OLD.channel_id OR NEW.method<>OLD.method THEN
  RAISE EXCEPTION 'provider attempt identity is immutable';
 END IF;
 IF OLD.state<>'processing' AND (NEW.state<>OLD.state OR NEW.material_type<>OLD.material_type OR NEW.material<>OLD.material OR NEW.provider_trade_no<>OLD.provider_trade_no) THEN
  RAISE EXCEPTION 'provider attempt outcome is immutable; use reconciliation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_attempt_guard BEFORE UPDATE ON provider_attempts FOR EACH ROW EXECUTE FUNCTION guard_provider_attempt();
CREATE TRIGGER provider_attempt_no_delete BEFORE DELETE ON provider_attempts FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
