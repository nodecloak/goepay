-- Existing legacy imports did not carry an original merchant registration
-- timestamp. Keep their immutable birth column intact and mark its meaning.
-- This flag is display metadata; it never authorizes identity or money work.
ALTER TABLE merchants ADD COLUMN registration_time_known boolean NOT NULL DEFAULT true;
UPDATE merchants SET registration_time_known=false
 WHERE profile->>'legacy_source' IN ('epay_pro','epay_release')
 AND COALESCE(profile->>'legacy_export','')<>'';
