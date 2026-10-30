-- 站内转账 拒绝原因 (and any funds review's rejection note). The original admin
-- internal-transfer page captured a 拒绝原因 prompt and stored it with the audit
-- event; GOEPAY's review already appends a ledger_request_events row per decision,
-- so the reason belongs on that immutable event, not on the request.
-- Nullable + no backfill: historical events keep NULL (no reason recorded), never
-- a fabricated one. The ledger_request_events_immutable trigger forbids UPDATE, so
-- a reason is only ever written at INSERT time with its decision. Forward-only.
ALTER TABLE ledger_request_events ADD COLUMN IF NOT EXISTS reason text;
