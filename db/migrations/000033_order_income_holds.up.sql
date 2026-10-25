SET CONSTRAINTS account_balance_guard IMMEDIATE;
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold'));
ALTER TABLE accounts ADD CONSTRAINT order_income_account_domain CHECK(kind<>'order_income_hold' OR owner_id>0);
SET CONSTRAINTS account_balance_guard DEFERRED;

CREATE TABLE order_income_holds (
 id bigserial PRIMARY KEY,
 order_id bigint NOT NULL REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 request_no text NOT NULL CHECK(length(request_no) BETWEEN 1 AND 128),
 merchant_net bigint NOT NULL CHECK(merchant_net>0),
 refund_merchant_amount bigint NOT NULL CHECK(refund_merchant_amount>=0),
 amount bigint NOT NULL CHECK(amount>0),
 state text NOT NULL DEFAULT 'held' CHECK(state IN ('held','released')),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 released_at timestamptz,
 CHECK(amount=merchant_net-refund_merchant_amount),
 CHECK((state='held' AND released_at IS NULL) OR (state='released' AND released_at IS NOT NULL)),
 UNIQUE(merchant_id,request_no)
);
CREATE UNIQUE INDEX order_income_one_active ON order_income_holds(order_id) WHERE state='held';
CREATE INDEX order_income_order_history ON order_income_holds(order_id,id DESC);
CREATE TABLE order_income_hold_events (
 id bigserial PRIMARY KEY,
 hold_id bigint NOT NULL REFERENCES order_income_holds(id),
 action text NOT NULL CHECK(action IN ('freeze','unfreeze')),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 128),
 actor_class text NOT NULL CHECK(actor_class IN ('super','operator')),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 UNIQUE(hold_id,action)
);
CREATE TRIGGER order_income_events_immutable BEFORE UPDATE OR DELETE ON order_income_hold_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER order_income_hold_no_delete BEFORE DELETE ON order_income_holds FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_order_income_hold() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.order_id,NEW.merchant_id,NEW.request_no,NEW.merchant_net,NEW.refund_merchant_amount,NEW.amount,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.order_id,OLD.merchant_id,OLD.request_no,OLD.merchant_net,OLD.refund_merchant_amount,OLD.amount,OLD.created_at) OR OLD.state='released' AND NEW IS DISTINCT FROM OLD THEN
 RAISE EXCEPTION 'immutable income hold identity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_income_hold_guard BEFORE UPDATE ON order_income_holds FOR EACH ROW EXECUTE FUNCTION guard_order_income_hold();
CREATE FUNCTION block_refund_while_income_held() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Serialize with freeze/unfreeze and read after any order-lock wait.
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=NEW.order_id AND state='held') THEN
 RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='order income held; unfreeze income before refund',CONSTRAINT='order_income_refund_blocked';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_income_refund_guard BEFORE INSERT OR UPDATE OF status ON refunds FOR EACH ROW EXECUTE FUNCTION block_refund_while_income_held();
