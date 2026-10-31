-- Each provider identity belongs to at most one merchant, including when
-- concurrent callbacks try to bind it. Existing duplicates stop migration so
-- they must be resolved explicitly instead of silently choosing an account.
CREATE UNIQUE INDEX merchant_connect_qq_uid_unique ON merchants ((profile->>'qq_uid')) WHERE COALESCE(profile->>'qq_uid','') <> '';
CREATE UNIQUE INDEX merchant_connect_wx_uid_unique ON merchants ((profile->>'wx_uid')) WHERE COALESCE(profile->>'wx_uid','') <> '';
CREATE UNIQUE INDEX merchant_connect_alipay_uid_unique ON merchants ((profile->>'alipay_uid')) WHERE COALESCE(profile->>'alipay_uid','') <> '';
CREATE UNIQUE INDEX merchant_connect_telegram_uid_unique ON merchants ((profile->>'telegram_uid')) WHERE COALESCE(profile->>'telegram_uid','') <> '';
