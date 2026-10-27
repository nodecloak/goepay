import { it, expect } from "vitest";
import {
  readScheduledTask,
  readScheduledCatalog,
  readScheduledRun,
  readScheduledHistory,
  scheduledTaskPath,
  scheduledTaskModeLabel,
  scheduledTaskChange,
  scheduledUTC8Input,
  type ScheduledDraft,
} from "../src/scheduled-tasks";
import { scheduler171Task, scheduler171Run } from "./scheduler171-fixtures";

it("keeps full run/revision IDs and rejects unknown secret or lease fields", () => {
  expect(readScheduledTask(scheduler171Task()).revision).toBe(
    "9007199254740993",
  );
  expect(readScheduledRun(scheduler171Run()).id).toBe("9007199254740993");
  for (const raw of [
    scheduler171Task({ revision: 9007199254740993 }),
    scheduler171Task({ revision: "01" }),
    scheduler171Task({ lease_token: "hidden" }),
    scheduler171Task({ params: { limit: 20, sql: "select" } }),
    scheduler171Task({ can_run: "true" }),
    scheduler171Task({ fingerprint: "raw-private" }),
    scheduler171Task({
      parameter_definitions: [
        {
          name: "limit",
          label: "x",
          type: "number",
          min: 1,
          max: 100,
          default: 20,
          required: true,
          secret: "hidden",
        },
      ],
    }),
  ])
    expect(() => readScheduledTask(raw)).toThrow();
  for (const raw of [
    scheduler171Run({ id: 9007199254740993 }),
    scheduler171Run({ id: "9223372036854775808" }),
    scheduler171Run({ lease_epoch: 1 }),
    scheduler171Run({ status: "success" }),
    scheduler171Run({ completed_at: "2026-10-26T02:00:00Z" }),
  ])
    expect(() => readScheduledRun(raw)).toThrow();
});
it("unconfigured legacy mode is distinct from disabled and completed execution", () => {
  const legacy = readScheduledTask(
    scheduler171Task({
      task: "notify",
      configured: false,
      control_mode: "legacy_managed",
      enabled: false,
      schedule: null,
      parameter_definitions: [],
      params: {},
      revision: "0",
      can_run: false,
      next_run_time: null,
    }),
  );
  expect(scheduledTaskModeLabel(legacy)).toContain("未接管");
  expect(
    scheduledTaskModeLabel(
      readScheduledTask(
        scheduler171Task({
          enabled: false,
          can_run: false,
          next_run_time: null,
        }),
      ),
    ),
  ).toContain("已关闭");
  for (const raw of [
    scheduler171Task({ configured: false }),
    scheduler171Task({ running: true }),
    scheduler171Task({ available: false, can_update: true }),
  ])
    expect(() => readScheduledTask(raw)).toThrow();
});
it("binds exact task paths, catalog uniqueness and history task scope", () => {
  expect(scheduledTaskPath("notify")).toBe("/scheduled-tasks/notify");
  for (const key of ["../notify", "notify?x=1", "sql", "Notify"])
    expect(() => scheduledTaskPath(key)).toThrow();
  expect(
    readScheduledCatalog({ items: [scheduler171Task()], total: 1 }),
  ).toHaveLength(1);
  for (const raw of [
    { items: [scheduler171Task(), scheduler171Task()], total: 2 },
    { items: [scheduler171Task()], total: 0 },
    { items: [], total: "0" },
  ])
    expect(() => readScheduledCatalog(raw)).toThrow();
  expect(
    readScheduledHistory(
      { items: [scheduler171Run()], total: 1, next_cursor: "9007199254740993" },
      "referral_daily",
    ).items[0].id,
  ).toBe("9007199254740993");
  expect(() =>
    readScheduledHistory(
      {
        items: [scheduler171Run({ task: "notify" })],
        total: 1,
        next_cursor: "0",
      },
      "referral_daily",
    ),
  ).toThrow();
});
it("converts original seconds/minutes/hours/days/once and UTC+8 without browser timezone drift", () => {
  const task = readScheduledTask(scheduler171Task());
  expect(scheduledUTC8Input("2026-10-26T02:00:00Z")).toBe(
    "2026-10-26T10:00:00",
  );
  for (const frequency of ["second", "minute", "hour", "day", "once"]) {
    const change = scheduledTaskChange(task, {
      enabled: true,
      frequency,
      interval: "1",
      executeAt: "2026-10-26T10:00:00",
      params: { limit: "20" },
    });
    expect(change.schedule.execute_at).toBe("2026-10-26T02:00:00.000Z");
    expect(change.schedule.frequency).toBe(frequency);
    expect(change.expected_fingerprint).toBe("a".repeat(64));
  }
  for (const update of [
    { interval: "0" },
    { interval: "1.5" },
    { interval: "01" },
    { frequency: "week" },
    { frequency: "once", interval: "2" },
    { executeAt: "2026-02-30T10:00:00" },
    { params: { limit: "101" } },
    { params: { limit: "1", extra: "1" } },
  ] as Partial<ScheduledDraft>[])
    expect(() =>
      scheduledTaskChange(task, {
        enabled: true,
        frequency: "minute",
        interval: "1",
        executeAt: "2026-10-26T10:00:00",
        params: { limit: "20" },
        ...update,
      }),
    ).toThrow();
});
