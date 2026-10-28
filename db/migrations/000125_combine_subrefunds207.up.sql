-- Per-sub-order allocation of one platform refund of a combined order. The
-- plan is fixed when first sent (deterministic out_refund_no per share), so a
-- retry or query always addresses the same provider refunds; the original
-- used date+rand refund numbers and lost progress on a partial failure.
CREATE TABLE combine_subrefunds207 (
 refund_id bigint NOT NULL REFERENCES refunds(id),
 order_id bigint NOT NULL,
 position smallint NOT NULL,
 amount bigint NOT NULL CHECK(amount>0),
 provider_refund_no text NOT NULL UNIQUE CHECK(provider_refund_no ~ '^[A-Za-z0-9_-]{1,64}$'),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','succeeded','failed','unknown')),
 PRIMARY KEY(refund_id,position),
 FOREIGN KEY(order_id,position) REFERENCES combine_suborders207(order_id,position)
);
CREATE FUNCTION combine_subrefunds207_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR (NEW.refund_id,NEW.order_id,NEW.position,NEW.amount,NEW.provider_refund_no) IS DISTINCT FROM (OLD.refund_id,OLD.order_id,OLD.position,OLD.amount,OLD.provider_refund_no)
 OR (OLD.state IN ('succeeded','failed') AND NEW.state<>OLD.state) THEN
 RAISE EXCEPTION 'combined sub-refund plan is append-only'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER combine_subrefunds207_original BEFORE UPDATE OR DELETE ON combine_subrefunds207 FOR EACH ROW EXECUTE FUNCTION combine_subrefunds207_original();
