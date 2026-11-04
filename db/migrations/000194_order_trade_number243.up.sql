-- Keep ordinary payment identifiers <=30 digits so combined child identifiers
-- (the unchanged parent plus a 1..50 position) still fit the 32-character limit.
-- The shared trade_no_seq and all existing identifiers are intentionally kept.
CREATE SEQUENCE IF NOT EXISTS order_trade_no_seq AS bigint
 MINVALUE 1 MAXVALUE 9999999999999999 START WITH 1 NO CYCLE;
