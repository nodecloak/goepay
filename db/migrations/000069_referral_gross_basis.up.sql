-- Explicit gross-based immediate rewards still spend only the original
-- order's platform fee. Existing policy rows and original terms are untouched.
ALTER TABLE referral_policies DROP CONSTRAINT referral_policies_basis_check;
ALTER TABLE referral_policies ADD CONSTRAINT referral_policies_basis_check CHECK(basis IN('fee','gross'));
