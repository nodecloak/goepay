-- Read paths use immutable successful-login subjects and transfer resolution
-- times. Neither index reinterprets a legacy username or mutable update time.
CREATE INDEX console_login_verified_subject_time ON console_login_audit(subject_id,created_at DESC)
 WHERE role='merchant' AND success AND subject_id IS NOT NULL;
CREATE INDEX transfers_resolved_reporting ON transfers(resolved_at,merchant_id,id)
 WHERE status='succeeded' AND resolved_at IS NOT NULL;
