-- Preserve the complete original resource catalogue and allow the independent
-- HMAC-authenticated native-scan setting. No existing rows or proof bytes change.
ALTER TABLE backoffice_records DROP CONSTRAINT backoffice_records_resource_check;
ALTER TABLE backoffice_records ADD CONSTRAINT backoffice_records_resource_check CHECK(resource IN ('groups','domains','invitation-codes','announcements','settings','channels','blacklist','collection-styles','profitsharing-rules','pay-types','pay-rolls','weixin-apps','onboarding-channels','onboarding-applications','subchannels','merchant-risk','complaints','complaint-tasks','risk-records','cron-tasks','settlement-settings','collection-codes','plugin-configs','template-configs','addon-configs','order-subject-policies','scan-js-settings202'));
