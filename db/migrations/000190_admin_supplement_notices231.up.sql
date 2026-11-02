-- System-wide complaints may have no verified merchant. NULL explicitly means
-- unknown; no arbitrary merchant is invented to satisfy a notification FK.
ALTER TABLE admin_business_notice_events ALTER COLUMN merchant_id DROP NOT NULL;
ALTER TABLE admin_business_notice_claims ALTER COLUMN merchant_id DROP NOT NULL;
ALTER TABLE admin_business_notice_events ADD CONSTRAINT admin_business_event_job231 UNIQUE(id,job_id);
ALTER TABLE admin_business_notice_claims ADD CONSTRAINT admin_business_claim_job231 FOREIGN KEY(event_id,job_id) REFERENCES admin_business_notice_events(id,job_id);
ALTER TABLE admin_business_notice_events DROP CONSTRAINT admin_business_notice_events_mode_check;
ALTER TABLE admin_business_notice_claims DROP CONSTRAINT admin_business_notice_claims_transport_check;
ALTER TABLE admin_business_notice_claims ADD CONSTRAINT admin_business_transport231 CHECK(transport IN('smtp','sms','robot','telegram','qqbot'));
DO $$ DECLARE c record;BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='admin_business_notice_events'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%scene%'
 LOOP EXECUTE format('ALTER TABLE admin_business_notice_events DROP CONSTRAINT %I',c.conname);END LOOP;
END $$;
ALTER TABLE admin_business_notice_events ADD CONSTRAINT admin_business_mode231 CHECK(mode IN('0','1','2','4','5') OR scene='complain_all' AND mode='3');
ALTER TABLE admin_business_notice_events ADD CONSTRAINT admin_business_source_shape231 CHECK(
 scene IN('regaudit','domain','complain_all','channellimit') AND audit_id IS NOT NULL AND request_event_id IS NULL
 OR scene IN('apply','internal_transfer') AND request_event_id IS NOT NULL AND audit_id IS NULL
);
ALTER TABLE admin_business_notice_events ADD CONSTRAINT admin_business_subject231 CHECK(merchant_id IS NOT NULL OR scene IN('complain_all','channellimit'));
CREATE UNIQUE INDEX admin_notification_source231 ON backoffice_audit(resource_id) WHERE resource='notification-sources231' AND actor_name='notification-source231' AND business_notice_scene IS NOT NULL;
ALTER TABLE backoffice_audit DROP CONSTRAINT admin_business_audit_source;
ALTER TABLE backoffice_audit ADD CONSTRAINT admin_business_audit_source CHECK(
 business_notice_scene IS NULL AND business_notice_transaction IS NULL
 OR business_notice_scene IS NOT NULL AND business_notice_transaction IS NOT NULL AND (
 actor_id>0 AND (business_notice_scene='regaudit' AND resource='enrollment' AND operation='register' AND actor_role='public' AND COALESCE(changes->>'pay'='2',false)
 OR business_notice_scene='domain' AND resource='domains' AND operation IN('create','update') AND actor_role='merchant')
 OR actor_role='system' AND actor_id=0 AND actor_name='notification-source231' AND resource='notification-sources231' AND operation='create' AND business_notice_scene IN('complain_all','channellimit')));
