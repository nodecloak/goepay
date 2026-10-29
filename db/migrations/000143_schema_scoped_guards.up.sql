-- Several wave-2 migrations (000131-000141) guard CREATE TRIGGER and ADD
-- CONSTRAINT with a catalog lookup by name only (pg_trigger.tgname,
-- pg_constraint.conname). Those catalogs span the whole database, so when
-- another schema of the same database already holds an object with that name
-- (the test suite migrates every case into its own schema), the guard skips the
-- object in this schema and the table is left without its immutability trigger
-- or its CHECK.
--
-- Those migrations are already applied on installs and cannot change, so this
-- migration re-checks each object against the table it belongs to (tgrelid and
-- conrelid resolve through search_path to the current schema) and creates only
-- what is missing. On a normal single-schema install every object exists and
-- nothing changes. Forward-only; rewrites no rows.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='console_users'::regclass AND conname='console_users_permissions_array') THEN
    ALTER TABLE console_users ADD CONSTRAINT console_users_permissions_array
      CHECK (permissions IS NULL OR jsonb_typeof(permissions) = 'array');
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='enrollment_phones134'::regclass AND tgname='enrollment_phone134_immutable') THEN
    CREATE TRIGGER enrollment_phone134_immutable BEFORE UPDATE OR DELETE ON enrollment_phones134
      FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='complaints133'::regclass AND tgname='complaints133_forward') THEN
    CREATE TRIGGER complaints133_forward BEFORE UPDATE OR DELETE ON complaints133
      FOR EACH ROW EXECUTE FUNCTION complaints133_forward();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='verification_codes134'::regclass AND tgname='verification_codes134_forward') THEN
    CREATE TRIGGER verification_codes134_forward BEFORE UPDATE OR DELETE ON verification_codes134
      FOR EACH ROW EXECUTE FUNCTION verification_codes134_forward();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='sms_test_requests134'::regclass AND tgname='sms_test_requests134_forward') THEN
    CREATE TRIGGER sms_test_requests134_forward BEFORE UPDATE OR DELETE ON sms_test_requests134
      FOR EACH ROW EXECUTE FUNCTION sms_test_requests134_forward();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='connect_login_states135'::regclass AND tgname='connect_login_states135_guard') THEN
    CREATE TRIGGER connect_login_states135_guard BEFORE UPDATE OR DELETE ON connect_login_states135
      FOR EACH ROW EXECUTE FUNCTION connect_login_states135_guard();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='connect_pending136'::regclass AND tgname='connect_pending136_guard') THEN
    CREATE TRIGGER connect_pending136_guard BEFORE UPDATE OR DELETE ON connect_pending136
      FOR EACH ROW EXECUTE FUNCTION connect_pending136_guard();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='merchant_complaint_events201'::regclass AND tgname='merchant_complaint_events201_immutable') THEN
    CREATE TRIGGER merchant_complaint_events201_immutable BEFORE UPDATE OR DELETE ON merchant_complaint_events201
      FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='merchant_complaint_event_claims201'::regclass AND tgname='merchant_complaint_event_claims201_immutable') THEN
    CREATE TRIGGER merchant_complaint_event_claims201_immutable BEFORE UPDATE OR DELETE ON merchant_complaint_event_claims201
      FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='merchant_complaint_event_results201'::regclass AND tgname='merchant_complaint_event_results201_immutable') THEN
    CREATE TRIGGER merchant_complaint_event_results201_immutable BEFORE UPDATE OR DELETE ON merchant_complaint_event_results201
      FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
  END IF;
END$$;
