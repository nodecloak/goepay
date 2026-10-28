-- 扫码红包 (original admin/user transfer_red.php, pre_transfer status=4): a
-- red packet is a deferred merchant transfer. It carries no recipient when
-- created; whoever claims it through the signed QR becomes the recipient of
-- one ordinary transfer (transfer_no RP<id>). Balance is reserved by that
-- transfer at claim time, so an unclaimed packet never strands funds (the
-- original deducted at creation and never refunded expired packets).
CREATE TABLE redpacks (
 id bigserial PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 out_biz_no text NOT NULL CHECK(out_biz_no ~ '^[0-9]{19}$'),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 type text NOT NULL CHECK(type IN ('wxpay','alipay')),
 amount bigint NOT NULL CHECK(amount>0),
 remark text NOT NULL CHECK(char_length(remark)<=32),
 created_by text NOT NULL CHECK(created_by IN ('admin','merchant')),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 status text NOT NULL DEFAULT 'claimable' CHECK(status IN ('claimable','claiming','succeeded','failed','expired')),
 expires_at timestamptz NOT NULL,
 claimer_hash bytea CHECK(claimer_hash IS NULL OR octet_length(claimer_hash)=32),
 claimed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(merchant_id,out_biz_no),
 CHECK((status='claimable' OR status='expired')=(claimer_hash IS NULL AND claimed_at IS NULL))
);
CREATE INDEX redpacks_claimable_expiry ON redpacks(expires_at) WHERE status='claimable';
CREATE FUNCTION redpacks_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR (NEW.id,NEW.merchant_id,NEW.out_biz_no,NEW.channel_id,NEW.type,NEW.amount,NEW.remark,NEW.created_by,NEW.actor_id,NEW.expires_at,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.out_biz_no,OLD.channel_id,OLD.type,OLD.amount,OLD.remark,OLD.created_by,OLD.actor_id,OLD.expires_at,OLD.created_at)
 OR (OLD.claimer_hash IS NOT NULL AND NEW.claimer_hash IS DISTINCT FROM OLD.claimer_hash)
 OR NOT (NEW.status=OLD.status OR (OLD.status='claimable' AND NEW.status IN ('claiming','expired')) OR (OLD.status='claiming' AND NEW.status IN ('succeeded','failed'))) THEN
 RAISE EXCEPTION 'red packet history is append-only'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER redpacks_original BEFORE UPDATE OR DELETE ON redpacks FOR EACH ROW EXECUTE FUNCTION redpacks_original();
