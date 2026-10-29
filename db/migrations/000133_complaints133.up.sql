-- W6 交易投诉同步/处理 (complaint sync + handling) store, gated at runtime by the
-- site setting complain_open (default off; the setting key is added by the
-- settings workstream and read by name).
--
-- Original: pre_complain (synced WeChat/Alipay complaints) + pre_complaintask
-- (per-channel sync task state with MAX_FAILURES auto-stop). GOEPAY keeps the
-- immutable-ledger rule: a complaint's provider identity, order linkage,
-- amount and first-seen time are append-only; only its status, last-seen time
-- and the merchant's local handling notes move forward. Refund-on-complaint is
-- never performed here — it flows through GOEPAY's existing refund path, which
-- writes its own ledger entry; this store only records that a complaint was
-- seen and handled.
--
-- Forward-only: this file is applied as-is by db/migrate.go; it is idempotent
-- and safe on existing installs (IF NOT EXISTS, guarded DO blocks). No .down.

CREATE TABLE IF NOT EXISTS complaints133 (
 id bigserial PRIMARY KEY,
 -- Provider complaint identity. (pay_type, third_id) is globally unique: the
 -- same complaint is upserted on re-sync, never duplicated.
 pay_type text NOT NULL CHECK(pay_type IN ('wxpay','alipay')),
 third_id text NOT NULL CHECK(third_id ~ '^[0-9A-Za-z_-]{1,128}$'),
 channel_id bigint NOT NULL,
 subchannel_id bigint NOT NULL DEFAULT 0,
 owner_id bigint REFERENCES merchants(id),
 -- Order linkage (nullable: a complaint may arrive before its order resolves).
 trade_nos text[] NOT NULL DEFAULT '{}',
 third_mch_id text,
 category text,
 title text,
 content text,
 complaint_times int NOT NULL DEFAULT 1 CHECK(complaint_times>=1),
 -- Integer cents; NULL stays unknown, never presented as zero money.
 amount_cents bigint CHECK(amount_cents IS NULL OR amount_cents>=0),
 -- Provider status string, verbatim (e.g. PENDING/PROCESSING for WeChat,
 -- MERCHANT_PROCESSING/FINISHED for Alipay). Mapped to a coarse local state.
 provider_status text NOT NULL,
 local_state text NOT NULL DEFAULT 'open' CHECK(local_state IN ('open','processing','done')),
 phone text,
 -- Verified upstream detail body kept for audit (never a credential-bearing URL).
 detail jsonb,
 -- Local handling bookkeeping (auto-handle flags applied, last reply etc.).
 handling jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(handling)='object'),
 first_seen_at timestamptz NOT NULL DEFAULT now(),
 last_seen_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(pay_type, third_id)
);
CREATE INDEX IF NOT EXISTS complaints133_owner ON complaints133(owner_id, id DESC);
CREATE INDEX IF NOT EXISTS complaints133_channel ON complaints133(channel_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS complaints133_state ON complaints133(local_state, last_seen_at DESC);

-- Append-only guard: identity, order linkage, amount and first-seen are frozen
-- once inserted; provider_status/local_state may only move forward
-- (open -> processing -> done is monotone by rank), last_seen_at only advances.
CREATE OR REPLACE FUNCTION complaints133_forward() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rank jsonb := '{"open":0,"processing":1,"done":2}'::jsonb;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'complaint records are append-only'; END IF;
 IF (NEW.pay_type,NEW.third_id,NEW.channel_id,NEW.first_seen_at) IS DISTINCT FROM (OLD.pay_type,OLD.third_id,OLD.channel_id,OLD.first_seen_at)
  OR NEW.trade_nos IS DISTINCT FROM OLD.trade_nos
  OR (OLD.amount_cents IS NOT NULL AND NEW.amount_cents IS DISTINCT FROM OLD.amount_cents) THEN
  RAISE EXCEPTION 'complaint identity, order linkage and amount are immutable';
 END IF;
 IF (rank->>NEW.local_state)::int < (rank->>OLD.local_state)::int THEN
  RAISE EXCEPTION 'complaint local_state cannot move backward';
 END IF;
 IF NEW.last_seen_at < OLD.last_seen_at THEN
  RAISE EXCEPTION 'complaint last_seen_at cannot move backward';
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='complaints133_forward') THEN
  CREATE TRIGGER complaints133_forward BEFORE UPDATE OR DELETE ON complaints133
  FOR EACH ROW EXECUTE FUNCTION complaints133_forward();
 END IF;
END $$;

-- Per-channel sync task state (pre_complaintask). Mutable operational state.
CREATE TABLE IF NOT EXISTS complaint_tasks133 (
 channel_id bigint PRIMARY KEY,
 enabled boolean NOT NULL DEFAULT true,
 -- Only sync complaints whose channel has seen traffic within `days` (0=all),
 -- a page of `num` (1..50) each run, every `frequency` minutes.
 days int NOT NULL DEFAULT 0 CHECK(days>=0),
 frequency int NOT NULL DEFAULT 6 CHECK(frequency>=1),
 num int NOT NULL DEFAULT 20 CHECK(num BETWEEN 1 AND 50),
 -- Consecutive-failure counter; at the MAX_FAILURES threshold (code: 10) the
 -- task auto-disables and the admin is notified, mirroring the original cron.
 consecutive_failures int NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0),
 last_run_at timestamptz,
 last_status text CHECK(last_status IS NULL OR last_status IN ('ok','failed')),
 next_run_at timestamptz,
 last_reason text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS complaint_tasks133_due ON complaint_tasks133(enabled, next_run_at) WHERE enabled;
