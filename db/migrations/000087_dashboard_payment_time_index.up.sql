-- Dashboard cash totals use verified payment time rather than order creation.
-- This bounded date projection must not force a scan of all pending orders.
CREATE INDEX orders_dashboard_paid_time ON orders(paid_at,merchant_id,type) INCLUDE(amount,platform_fee) WHERE status='paid';
