-- Fixed 27-digit parents keep WeChat combined children at 28..29 digits and
-- ordinary ChinaUMS bill numbers within 31 digits after its four-digit prefix.
-- The shared trade_no_seq and all existing identifiers are intentionally kept.
CREATE SEQUENCE IF NOT EXISTS order_trade_no_seq AS bigint
 MINVALUE 1 MAXVALUE 9999999999999 START WITH 1 NO CYCLE;
