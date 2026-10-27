-- NULL preserves the existing strict signature_algorithm contract. The original
-- keytype modes are opt-in and do not rewrite keys, revisions or legacy proofs.
ALTER TABLE merchants ADD COLUMN keytype smallint CHECK (keytype IN (0, 1));
