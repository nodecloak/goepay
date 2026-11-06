import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { planBody, type PlanDraft } from "../src/settlement-plans";
import { ApiClient } from "../src/api";
import SettlementPlansPage from "../src/components/SettlementPlansPage.vue";

it("uses the explicit original threshold mode and bounded minute/second cadence without changing ordinary generation", () => {
  const draft = {
    name: "余额自动转账",
    mode: "balance_threshold",
    frequency: "minute",
    interval: "1",
    anchor_at: "2026-10-24T00:00:00+08:00",
    execute_hour: "0",
    execute_minute: "0",
    execute_second: "0",
    merchant_ids: "2,3",
  } as PlanDraft;
  expect(planBody(draft).mode).toBe("balance_threshold");
  expect(
    planBody({ ...draft, frequency: "second", interval: "10" }).schedule,
  ).toMatchObject({ frequency: "second", interval: "10" });
  for (const change of [
    { mode: "generate_only" },
    { mode: "auto_payout" },
    { frequency: "minute", interval: "1441" },
    { frequency: "second", interval: "9" },
    { frequency: "second", interval: "3601" },
  ])
    expect(() => planBody({ ...draft, ...change })).toThrow();
  expect(
    planBody({ ...draft, mode: "generate_only", frequency: "hour" }).mode,
  ).toBe("generate_only");
});

it("reads the threshold plan, explains its original five-wallet rules and locks an authorized business mode", async () => {
  const plan = {
    id: "4",
    revision: "2",
    name: "商户余额自动转账",
    mode: "balance_threshold",
    enabled: true,
    last_authorization_id: "8",
    merchant_ids: ["2"],
    schedule: {
      frequency: "minute",
      interval: "1",
      anchor_at: "2026-10-24T00:00:00+08:00",
      execute_hour: 0,
      execute_minute: 0,
      execute_second: 0,
    },
  };
  const request = vi.fn((p: string) =>
    Promise.resolve(
      p === "/settlement-plans/4"
        ? plan
        : { items: p.includes("/runs?") ? [] : [plan], next_cursor: "" },
    ),
  );
  const w = mount(SettlementPlansPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  await w.find('[aria-label="查看计划 4"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("每次最多选 5");
  expect(w.text()).toContain("结算全部余额");
  expect(w.text()).not.toContain("计划身份或模式不匹配");
  await w
    .findAll("button")
    .find((b) => b.text() === "修改计划定义")!
    .trigger("click");
  expect(
    w.find('[aria-label="计划业务模式"]').attributes("disabled"),
  ).toBeDefined();
  expect(
    request.mock.calls.some(
      ([p]) =>
        p.includes("/authorizations") && !p.includes("auto-authorization"),
    ),
  ).toBe(false);
  w.unmount();
});
