-- 000140: indexes the complaints133 console list needs.
--
-- The W6 交易投诉 list filters by the linked order number (a value inside the
-- trade_nos text[] array) and scopes to a merchant owner. 000133 shipped
-- (owner_id, id DESC), (channel_id, last_seen_at DESC) and
-- (local_state, last_seen_at DESC); the only access path not already covered is
-- the 关联订单 filter, which does an array-containment lookup (trade_nos @> or
-- = ANY). A GIN index over trade_nos serves it. Forward-only, idempotent, no
-- .down — matches db/migrate.go's apply-as-is contract.

CREATE INDEX IF NOT EXISTS complaints133_trade_nos ON complaints133 USING gin(trade_nos);
