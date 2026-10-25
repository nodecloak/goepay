-- Read-only T+1 calculation; recharge_intents.order_id is already unique.
CREATE INDEX orders_t1_paid ON orders(merchant_id,paid_at,id) INCLUDE(amount) WHERE status='paid';
CREATE INDEX refunds_t1_succeeded ON refunds(order_id) INCLUDE(amount) WHERE status='succeeded';
