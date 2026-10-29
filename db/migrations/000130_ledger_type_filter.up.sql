-- 资金明细 original record-type filter support.
--
-- The console 资金明细 list now maps each ledger journal to its original
-- pre_record.type (订单收入, 代付, 自动结算, …) and offers a type filter. The
-- projection and filter classify a row by its journals.event_key PREFIX
-- (payment:%, request:%, transfer:%, deposit:%, membership:%, …) — see
-- internal/server/ledger_reporting_type.go. journals.event_key already carries
-- a UNIQUE btree, but under a non-C database collation that index cannot serve
-- a `LIKE 'prefix%'` scan. Add a text_pattern_ops index so the prefix filter
-- stays index-assisted regardless of the server collation.
--
-- Forward-only and idempotent: CREATE INDEX IF NOT EXISTS is safe on every
-- existing install and adds no column, constraint or row. No per-merchant
-- remain_money column is needed — the settlement reserve lives in the existing
-- merchants.profile JSONB (profile->>'remain_money'), read live at scheduled
-- settlement time.
CREATE INDEX IF NOT EXISTS journals_event_key_prefix_idx
 ON journals (event_key text_pattern_ops);
