-- Open the generic collection/accounting path to the full original pay-type
-- catalog (pre_type: alipay, wxpay, qqpay, bank, jdpay, paypal, douyinpay; some
-- plugins also serve ecny). Provider-specific products (sub-channels, settlement
-- funding, scan/barcode recovery, red packets) keep their narrower constraints.
--
-- Only (a)-category CHECK constraints — order-routing assignment, runtime
-- channel policies (fees) and limits, runtime parent-channel accounting and
-- referral profit costs — are widened. Each inline CHECK is auto-named
-- <table>_<column>_check.
ALTER TABLE provider_route_assignments DROP CONSTRAINT provider_route_assignments_pay_type_check;
ALTER TABLE provider_route_assignments ADD CONSTRAINT provider_route_assignments_pay_type_check CHECK(pay_type IN ('alipay','wxpay','qqpay','bank','jdpay','paypal','douyinpay','ecny'));

ALTER TABLE runtime_channel_policies DROP CONSTRAINT runtime_channel_policies_pay_type_check;
ALTER TABLE runtime_channel_policies ADD CONSTRAINT runtime_channel_policies_pay_type_check CHECK(pay_type IN ('alipay','wxpay','qqpay','bank','jdpay','paypal','douyinpay','ecny'));

ALTER TABLE runtime_channel_limits DROP CONSTRAINT runtime_channel_limits_pay_type_check;
ALTER TABLE runtime_channel_limits ADD CONSTRAINT runtime_channel_limits_pay_type_check CHECK(pay_type IN ('alipay','wxpay','qqpay','bank','jdpay','paypal','douyinpay','ecny'));

ALTER TABLE runtime_parent_channels DROP CONSTRAINT runtime_parent_channels_pay_type_check;
ALTER TABLE runtime_parent_channels ADD CONSTRAINT runtime_parent_channels_pay_type_check CHECK(pay_type IN ('alipay','wxpay','qqpay','bank','jdpay','paypal','douyinpay','ecny'));

ALTER TABLE runtime_parent_channel_accounts DROP CONSTRAINT runtime_parent_channel_accounts_pay_type_check;
ALTER TABLE runtime_parent_channel_accounts ADD CONSTRAINT runtime_parent_channel_accounts_pay_type_check CHECK(pay_type IN ('alipay','wxpay','qqpay','bank','jdpay','paypal','douyinpay','ecny'));

ALTER TABLE referral_profit_costs DROP CONSTRAINT referral_profit_costs_provider_check;
ALTER TABLE referral_profit_costs ADD CONSTRAINT referral_profit_costs_provider_check CHECK(provider IN('alipay','wxpay','qqpay','bank','jdpay','paypal','douyinpay','ecny'));

-- The original pre_type catalog is not seeded here: migrations never add rows
-- to existing tables (the history-preservation tests in db/ hold every older
-- table byte-for-byte). Its defaults live in code instead (pre_type status:
-- alipay and wxpay open, the other types closed until a 支付方式 record
-- enables them) — see internal/server/pay_type_catalog.go.
