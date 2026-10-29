-- B11 交易投诉 同步任务 parity: widen the per-run page size to epay_pro's range.
--
-- epay_pro's 同步任务 editor allows 每次同步获取数量 (per-run num) 20..200 and
-- 获取频率 >= 1 minute, with 获取条件 (days, 0 = 不限) already present on
-- complaint_tasks133 (migration 000133). GOEPAY originally clamped num to 1..50
-- (the DB CHECK here and the provider sync clamp). To let admins set the same
-- values Pro exposes, relax the CHECK to 1..200 (keeping a floor of 1 so an
-- existing row's default of 20 stays valid and nothing already stored breaks).
--
-- Forward-only, idempotent: drop the old constraint if present and re-add the
-- widened one. Applies cleanly after 000143; safe on installs that never had
-- the constraint under this name.

DO $$
BEGIN
  IF to_regclass('complaint_tasks133') IS NULL THEN
    RETURN;
  END IF;
  -- Drop whatever num CHECK currently guards the column (its auto-name is
  -- complaint_tasks133_num_check), then add the widened one under a stable name.
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'complaint_tasks133_num_check') THEN
    ALTER TABLE complaint_tasks133 DROP CONSTRAINT complaint_tasks133_num_check;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'complaint_tasks133_num_range') THEN
    ALTER TABLE complaint_tasks133 DROP CONSTRAINT complaint_tasks133_num_range;
  END IF;
  ALTER TABLE complaint_tasks133
    ADD CONSTRAINT complaint_tasks133_num_range CHECK (num BETWEEN 1 AND 200);
END $$;
