-- Read models only; no historical order/ledger mutation or money-policy change.
-- Merchant order/date access already has orders_merchant_created (migration1).
CREATE INDEX orders_console_created ON orders(created_at DESC,id DESC);
CREATE INDEX refunds_console_merchant_created ON refunds(merchant_id,created_at DESC,id DESC);
CREATE INDEX transfers_console_merchant_id ON transfers(merchant_id,id DESC);
CREATE INDEX provider_attempts_console_channel ON provider_attempts(channel_id,order_id);
CREATE INDEX order_reporting_console_buyer ON order_reporting(buyer,order_id) WHERE buyer<>'';
CREATE INDEX order_reporting_console_ip ON order_reporting(client_ip,order_id) WHERE client_ip<>'';
