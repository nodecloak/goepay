-- Preserve legacy SMTP proofs byte-for-byte. New route proofs bind transport,
-- target and sending profile before any external request (including token I/O).
ALTER TABLE channel_stop_notice_claims
 ADD COLUMN route_version smallint NOT NULL DEFAULT 0,
 ADD COLUMN route_mode text NOT NULL DEFAULT '',
 ADD COLUMN route_scope_hash bytea,
 ADD COLUMN route_hash bytea,
 ADD CONSTRAINT channel_notice_route_shape CHECK (
  (route_version=0 AND route_mode='' AND route_scope_hash IS NULL AND route_hash IS NULL)
  OR (route_version=1 AND route_mode IN('smtp','robot','telegram','qqbot')
      AND route_scope_hash IS NOT NULL AND octet_length(route_scope_hash)=32
      AND route_hash IS NOT NULL AND octet_length(route_hash)=32)
 );
