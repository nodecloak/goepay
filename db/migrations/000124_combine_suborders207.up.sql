-- WeChat Pay v3 direct-merchant combined-order sub-orders (original
-- pre_suborder: trade_no, sub_trade_no, api_trade_no, money, refundmoney,
-- status). Amounts are cents; positions 1..50; the shares always sum to the
-- order's charged amount (checked at commit). Identity and amount are
-- immutable; status only moves forward; refunds only grow and never exceed
-- the paid share.
CREATE TABLE combine_suborders207 (
 order_id bigint NOT NULL REFERENCES orders(id),
 position smallint NOT NULL CHECK(position BETWEEN 1 AND 50),
 sub_trade_no text NOT NULL UNIQUE CHECK(sub_trade_no ~ '^[0-9A-Za-z_*|-]{6,32}$'),
 amount bigint NOT NULL CHECK(amount>0),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','paid','closed')),
 transaction_id text CHECK(transaction_id IS NULL OR transaction_id ~ '^[0-9A-Za-z_-]{1,64}$'),
 refunded_amount bigint NOT NULL DEFAULT 0 CHECK(refunded_amount>=0 AND refunded_amount<=amount),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 paid_at timestamptz,
 PRIMARY KEY(order_id,position),
 CHECK((status='paid')=(transaction_id IS NOT NULL AND paid_at IS NOT NULL)),
 CHECK(refunded_amount=0 OR status='paid')
);
CREATE FUNCTION combine_suborders207_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR (NEW.order_id,NEW.position,NEW.sub_trade_no,NEW.amount,NEW.created_at) IS DISTINCT FROM (OLD.order_id,OLD.position,OLD.sub_trade_no,OLD.amount,OLD.created_at)
 OR (OLD.status<>'pending' AND NEW.status<>OLD.status)
 OR (OLD.transaction_id IS NOT NULL AND NEW.transaction_id IS DISTINCT FROM OLD.transaction_id)
 OR (OLD.paid_at IS NOT NULL AND NEW.paid_at IS DISTINCT FROM OLD.paid_at)
 OR NEW.refunded_amount<OLD.refunded_amount THEN
 RAISE EXCEPTION 'combined sub-order history is append-only'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER combine_suborders207_original BEFORE UPDATE OR DELETE ON combine_suborders207 FOR EACH ROW EXECUTE FUNCTION combine_suborders207_original();
CREATE FUNCTION combine_suborders207_balanced() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE total bigint; subs bigint; n int; maxpos int; BEGIN
 SELECT COALESCE(o.actual_charge_amount104,o.amount) INTO total FROM orders o WHERE o.id=NEW.order_id;
 SELECT sum(amount),count(*),max(position) INTO subs,n,maxpos FROM combine_suborders207 WHERE order_id=NEW.order_id;
 IF subs<>total OR n<3 OR maxpos<>n THEN RAISE EXCEPTION 'combined sub-orders must be 3..50 contiguous shares summing to the order amount'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER combine_suborders207_balanced AFTER INSERT ON combine_suborders207 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION combine_suborders207_balanced();
