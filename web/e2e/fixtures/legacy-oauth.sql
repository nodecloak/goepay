-- This data exists only in this runner's new synthetic database, before the
-- API starts. The browser must discover, cancel, then clear the legacy secret.
BEGIN;
DO $$
DECLARE
    fixture record;
BEGIN
    IF current_database() !~ '^businessflow225_20261102_[a-f0-9]{32}$' THEN
        RAISE EXCEPTION 'owned synthetic database required';
    END IF;
    IF EXISTS (SELECT 1 FROM connect_login_credentials) THEN
        RAISE EXCEPTION 'OAuth vault must be uninitialized';
    END IF;
    FOR fixture IN SELECT * FROM (VALUES
        ('login_qq', '1'),
        ('login_qq_appid', 'synthetic-legacy-qq-app'),
        ('login_qq_appkey', 'synthetic-legacy-qq-key'),
        ('login_tg', '1'),
        ('login_tg_bot_token', '123:synthetic-legacy-telegram-key')
    ) AS slots(key,value) LOOP
        UPDATE backoffice_records
        SET body=body||jsonb_build_object('value',fixture.value)
        WHERE resource='settings' AND body->>'key'=fixture.key;
        IF NOT FOUND THEN
            INSERT INTO backoffice_records(resource,body)
            VALUES('settings',jsonb_build_object('key',fixture.key,'value',fixture.value));
        END IF;
        IF (SELECT count(*) FROM backoffice_records WHERE resource='settings' AND body->>'key'=fixture.key) <> 1 THEN
            RAISE EXCEPTION 'ambiguous synthetic legacy setting';
        END IF;
    END LOOP;
END $$;
COMMIT;
SELECT json_build_object('uninitialized_vault_rows',count(*),'synthetic_only',true) FROM connect_login_credentials;
