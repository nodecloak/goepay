-- 商家转账（新） payee confirmation (original transfer.ext / settle.transfer_ext
-- = package_info): a WeChat bill in WAIT_USER_CONFIRM carries one package that
-- the payee confirms in WeChat with requestMerchantTransfer. It is fixed for
-- the bill, so it is recorded once and never changed.
CREATE TABLE wechat_transfer_packages (
 transfer_id bigint PRIMARY KEY REFERENCES transfers(id),
 package_info text NOT NULL CHECK(char_length(package_info) BETWEEN 1 AND 2048 AND package_info !~ '[[:cntrl:]]'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION wechat_transfer_packages_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'wechat transfer package is immutable';
END $$;
CREATE TRIGGER wechat_transfer_packages_original BEFORE UPDATE OR DELETE ON wechat_transfer_packages FOR EACH ROW EXECUTE FUNCTION wechat_transfer_packages_original();
