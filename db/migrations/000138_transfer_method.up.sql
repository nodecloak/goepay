-- Plaintext transfer method (alipay/wxpay/qqpay/bank) for the merchant console
-- 付款方式 filter. The method is a non-sensitive rail classification, distinct
-- from the encrypted recipient (account/name/remark stay in recipient_cipher).
-- Nullable + no backfill: historical rows keep NULL and are matched only by the
-- unfiltered (全部) view, never silently reclassified. Forward-only, idempotent.
ALTER TABLE provider_transfer_attempts ADD COLUMN IF NOT EXISTS method text;
CREATE INDEX IF NOT EXISTS provider_transfer_attempts_method ON provider_transfer_attempts(method) WHERE method IS NOT NULL;
