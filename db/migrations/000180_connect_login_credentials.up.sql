-- 快捷登录 (original admin/set.php mod=oauth) secrets. The provider app
-- secrets (QQ/微信/支付宝 appkey and the Telegram bot token) are NOT plaintext
-- settings: they are stored AEAD-encrypted here, write-only, in a single
-- versioned row, exactly like proxy_credentials / captcha_credentials203.
-- Everything non-secret (enable switches, the public appids, the relay API
-- url) stays an ordinary backoffice setting.
CREATE TABLE connect_login_credentials (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL CHECK(revision>0),
 qq_cipher bytea NOT NULL,
 wx_cipher bytea NOT NULL,
 alipay_cipher bytea NOT NULL,
 tg_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
