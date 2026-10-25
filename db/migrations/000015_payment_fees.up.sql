ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved'));
ALTER TABLE accounts ADD CONSTRAINT platform_fee_nonnegative CHECK(kind NOT IN ('fees','fee_reserved') OR balance>=0);
INSERT INTO accounts(owner_id,kind) VALUES(0,'fees'),(0,'fee_reserved') ON CONFLICT DO NOTHING;
ALTER TABLE orders ADD COLUMN fee_policy text NOT NULL DEFAULT 'none' CHECK(fee_policy IN ('none','merchant_deduct'));
ALTER TABLE orders ADD COLUMN fee_rate_bps integer NOT NULL DEFAULT 0 CHECK(fee_rate_bps BETWEEN 0 AND 10000);
ALTER TABLE orders ADD COLUMN fee_group_id bigint NOT NULL DEFAULT 0 CHECK(fee_group_id>=0);
ALTER TABLE orders ADD COLUMN merchant_amount bigint;
ALTER TABLE orders ADD COLUMN platform_fee bigint NOT NULL DEFAULT 0 CHECK(platform_fee>=0);
ALTER TABLE orders ADD COLUMN refund_fee_payer text NOT NULL DEFAULT 'platform' CHECK(refund_fee_payer IN ('platform','merchant'));
UPDATE orders SET merchant_amount=amount;
ALTER TABLE orders ALTER COLUMN merchant_amount SET NOT NULL;
ALTER TABLE orders ADD CONSTRAINT order_fee_balanced CHECK(merchant_amount>=0 AND merchant_amount+platform_fee=amount);
CREATE OR REPLACE FUNCTION order_fee_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.merchant_amount IS NULL THEN NEW.merchant_amount:=NEW.amount; END IF; RETURN NEW; END IF;
 IF (NEW.amount,NEW.merchant_amount,NEW.platform_fee,NEW.fee_policy,NEW.fee_rate_bps,NEW.fee_group_id,NEW.refund_fee_payer) IS DISTINCT FROM (OLD.amount,OLD.merchant_amount,OLD.platform_fee,OLD.fee_policy,OLD.fee_rate_bps,OLD.fee_group_id,OLD.refund_fee_payer) THEN
 RAISE EXCEPTION 'immutable order fee snapshot'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER order_fee_snapshot_immutable BEFORE INSERT OR UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION order_fee_snapshot_guard();
ALTER TABLE refunds ADD COLUMN merchant_amount bigint;
ALTER TABLE refunds ADD COLUMN fee_amount bigint NOT NULL DEFAULT 0 CHECK(fee_amount>=0);
UPDATE refunds SET merchant_amount=amount;
ALTER TABLE refunds ALTER COLUMN merchant_amount SET NOT NULL;
ALTER TABLE refunds ADD CONSTRAINT refund_allocation_balanced CHECK(merchant_amount>=0 AND merchant_amount+fee_amount=amount);
CREATE OR REPLACE FUNCTION refund_allocation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.merchant_amount IS NULL THEN NEW.merchant_amount:=NEW.amount; END IF; RETURN NEW; END IF;
 IF (NEW.amount,NEW.merchant_amount,NEW.fee_amount,NEW.order_id,NEW.merchant_id,NEW.refund_no) IS DISTINCT FROM (OLD.amount,OLD.merchant_amount,OLD.fee_amount,OLD.order_id,OLD.merchant_id,OLD.refund_no) THEN
 RAISE EXCEPTION 'immutable refund allocation'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER refund_allocation_immutable BEFORE INSERT OR UPDATE ON refunds FOR EACH ROW EXECUTE FUNCTION refund_allocation_guard();
