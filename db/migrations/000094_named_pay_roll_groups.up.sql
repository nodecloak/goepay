-- Old rows retain their original default behaviour and exact body/proofs.
-- Only an explicit JSON false makes an enabled roll a named, non-default group.
DROP INDEX pay_roll_active_scope;
CREATE UNIQUE INDEX pay_roll_active_scope ON backoffice_records((body->>'merchant_id'),(body->>'type'))
 WHERE resource='pay-rolls' AND body ? 'merchant_id' AND body ? 'type'
 AND body->'enabled'='true'::jsonb
 AND body->'default_route' IS DISTINCT FROM 'false'::jsonb;
