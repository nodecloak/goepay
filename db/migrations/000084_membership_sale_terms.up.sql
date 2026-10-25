-- Freeze the actual sales quantity and calendar period on new purchases.
-- Historical rows remain NULL and retain their original private proof format.
ALTER TABLE membership_purchases ADD COLUMN sale_terms jsonb CHECK(sale_terms IS NULL OR jsonb_typeof(sale_terms)='object');
ALTER TABLE membership_purchases DROP CONSTRAINT membership_purchases_duration_days_check;
ALTER TABLE membership_purchases ADD CONSTRAINT membership_purchases_duration_days_check CHECK(duration_days BETWEEN 0 AND 3660000);
