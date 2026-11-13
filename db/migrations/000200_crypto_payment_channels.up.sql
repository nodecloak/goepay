-- Collection channels share the public numeric namespace with managed CNY
-- channels, while their execution, ledger and settlement asset remain USDT.
CREATE SEQUENCE IF NOT EXISTS runtime_parent_channel_ids AS bigint;
CREATE TABLE crypto_channels (
 id bigint PRIMARY KEY CHECK(id>0),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 network text NOT NULL CHECK(network IN ('tron','ethereum')),
 enabled boolean NOT NULL DEFAULT false,
 archived boolean NOT NULL DEFAULT false,
 revision bigint NOT NULL CHECK(revision>0),
 fee_bps bigint NOT NULL CHECK(fee_bps BETWEEN 0 AND 10000),
 min_amount bigint NOT NULL DEFAULT 0 CHECK(min_amount BETWEEN 0 AND 9000000000000000),
 max_amount bigint NOT NULL DEFAULT 0 CHECK(max_amount BETWEEN 0 AND 9000000000000000),
 day_limit bigint NOT NULL DEFAULT 0 CHECK(day_limit BETWEEN 0 AND 9000000000000000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(max_amount=0 OR max_amount>=min_amount),
 CHECK(NOT archived OR NOT enabled)
);
CREATE UNIQUE INDEX crypto_channel_names ON crypto_channels(lower(name)) WHERE NOT archived;
CREATE TABLE crypto_channel_grants (
 channel_id bigint NOT NULL REFERENCES crypto_channels(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 PRIMARY KEY(channel_id,merchant_id)
);
CREATE TABLE crypto_channel_requests (
 actor_id bigint NOT NULL CHECK(actor_id>0),
 request_id text NOT NULL CHECK(request_id ~ '^[a-f0-9]{32}$'),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 channel_id bigint NOT NULL REFERENCES crypto_channels(id),
 revision bigint NOT NULL CHECK(revision>0),
 PRIMARY KEY(actor_id,request_id)
);
ALTER TABLE crypto_invoices
 ADD COLUMN channel_id bigint REFERENCES crypto_channels(id),
 ADD COLUMN channel_revision bigint,
 ADD COLUMN fee_bps bigint,
 ADD COLUMN fee_source text NOT NULL DEFAULT 'legacy_network',
 ADD COLUMN channel_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
 ADD CONSTRAINT crypto_invoice_channel_snapshot CHECK((
  (channel_id IS NULL AND channel_revision IS NULL AND fee_bps IS NULL AND fee_source='legacy_network' AND channel_snapshot='{}'::jsonb)
  OR (channel_id>0 AND channel_revision>0 AND fee_bps BETWEEN 0 AND 10000 AND fee_source IN ('channel','group') AND jsonb_typeof(channel_snapshot)='object' AND channel_snapshot<>'{}'::jsonb)
 ) IS TRUE);
CREATE INDEX crypto_channel_daily_invoices ON crypto_invoices(channel_id,created_at) WHERE channel_id IS NOT NULL;
CREATE FUNCTION crypto_invoice_route_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(OLD.channel_id,OLD.channel_revision,OLD.fee_bps,OLD.fee_source,OLD.channel_snapshot,OLD.network,OLD.address,OLD.amount,OLD.fee,OLD.cny_amount)
    IS DISTINCT FROM ROW(NEW.channel_id,NEW.channel_revision,NEW.fee_bps,NEW.fee_source,NEW.channel_snapshot,NEW.network,NEW.address,NEW.amount,NEW.fee,NEW.cny_amount)
 THEN RAISE EXCEPTION 'crypto original amount and channel route are immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER crypto_invoice_route_immutable BEFORE UPDATE ON crypto_invoices FOR EACH ROW EXECUTE FUNCTION crypto_invoice_route_immutable();
