import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { it, expect } from "vitest";
import {
  readScheduledCatalog,
  readScheduledTask,
  readScheduledHistory,
  readScheduledRun,
} from "../src/scheduled-tasks";
const fixture = JSON.parse(
  readFileSync(
    resolve(process.cwd(), "e2e/scheduler171-http-fixtures.json"),
    "utf8",
  ),
);
it("consumes actual synthetic PG/HTTP catalog, terminal history and full pending ID without invented states", () => {
  expect(readScheduledCatalog(fixture.unconfigured_catalog)).toHaveLength(13);
  expect(readScheduledTask(fixture.completed_task).last_status).toBe("success");
  expect(readScheduledTask(fixture.saved_disabled_task).enabled).toBe(false);
  const completed = readScheduledHistory(
    fixture.completed_history,
    "referral_daily",
  );
  expect(completed.items[0].id).toBe("9007199254740993");
  expect(completed.items[0].started_at).not.toBeNull();
  expect(completed.items[0].completed_at).not.toBeNull();
  const pending = readScheduledRun(
    fixture.manual_pending_run,
    "referral_daily",
  );
  expect(pending.id).toBe("9007199254740994");
  expect(pending.status).toBe("pending");
  expect(pending.started_at).toBeNull();
  expect(pending.completed_at).toBeNull();
  expect(readScheduledTask(fixture.manual_pending_task).can_run).toBe(false);
  expect(
    readScheduledHistory(fixture.manual_pending_history, "referral_daily")
      .items[0].id,
  ).toBe(pending.id);
});
