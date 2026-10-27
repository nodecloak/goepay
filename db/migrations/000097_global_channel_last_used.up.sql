-- New -4 selections share each actual parent's last-use fact across groups.
-- Historical routes and group counters are not backfilled or rewritten.
CREATE TABLE provider_channel_last_used (
 channel_id bigint PRIMARY KEY CHECK(channel_id>0),
 merchant_id bigint NOT NULL,
 out_trade_no text NOT NULL,
 last_used_millis bigint NOT NULL CHECK(last_used_millis>=0),
 FOREIGN KEY(merchant_id,out_trade_no) REFERENCES provider_route_assignments(merchant_id,out_trade_no)
);

CREATE FUNCTION guard_provider_channel_last_used() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_channel bigint; source_millis bigint; source_policy text;
BEGIN
 SELECT channel_id,floor(extract(epoch FROM created_at)*1000)::bigint,
        rule_snapshot->'payment_info'->pay_type->>'channel'
 INTO source_channel,source_millis,source_policy
 FROM provider_route_assignments WHERE merchant_id=NEW.merchant_id AND out_trade_no=NEW.out_trade_no;
 IF NOT FOUND OR source_channel<>NEW.channel_id OR source_millis<>NEW.last_used_millis OR source_policy IS DISTINCT FROM '-4' THEN
  RAISE EXCEPTION 'global channel use requires its original sequential route';
 END IF;
 IF TG_OP='UPDATE' AND NEW.channel_id<>OLD.channel_id THEN
  RAISE EXCEPTION 'global channel identity cannot change';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_channel_last_used_source BEFORE INSERT OR UPDATE ON provider_channel_last_used
 FOR EACH ROW EXECUTE FUNCTION guard_provider_channel_last_used();
CREATE TRIGGER provider_channel_last_used_no_delete BEFORE DELETE ON provider_channel_last_used
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
