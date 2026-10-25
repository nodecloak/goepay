import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import SettlementPlansPage from "../src/components/SettlementPlansPage.vue";
import SettlementSchedulePage from "../src/components/SettlementSchedulePage.vue";
const plan = {
  id: "4",
  revision: "1",
  name: "p",
  mode: "generate_only",
  enabled: true,
  last_authorization_id: "8",
  merchant_ids: ["2"],
  schedule: {
    frequency: "day",
    interval: "1",
    anchor_at: "2026-10-24T00:00:00Z",
    execute_hour: 9,
    execute_minute: 0,
    execute_second: 0,
  },
};
it("does not display a delayed run detail after selecting a different plan", async () => {
  let resolveDetail: (x: unknown) => void = () => {};
  const second = { ...plan, id: "5", name: "second" };
  const request = vi.fn((path: string) =>
    path === "/settlement-plan-runs/9"
      ? new Promise((r) => (resolveDetail = r))
      : Promise.resolve(
          path === "/settlement-plans/4"
            ? plan
            : path === "/settlement-plans/5"
              ? second
              : {
                  items: path.includes("/runs?")
                    ? [{ id: "9", plan_id: "4", state: "completed" }]
                    : [plan, second],
                  next_cursor: "",
                },
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
  await w
    .findAll("button")
    .find((b) => b.text() === "运行 9")!
    .trigger("click");
  await w.find('[aria-label="查看计划 5"]').trigger("click");
  await flushPromises();
  resolveDetail({
    id: "9",
    plan_id: "4",
    state: "completed",
    items: [{ id: "1", reason: "wrong_old_plan" }],
  });
  await flushPromises();
  expect(w.text()).not.toContain("wrong_old_plan");
  w.unmount();
});
it("keeps emergency revocation available after an uncertain scan without repeating the scan", async () => {
  const request = vi.fn((path: string) =>
    Promise.resolve(
      path === "/settlement-plans/4"
        ? plan
        : { items: [plan], next_cursor: "" },
    ),
  );
  const requestResult = vi.fn((path: string) =>
    path.endsWith("/runs")
      ? Promise.reject(new Error("lost response"))
      : Promise.resolve({ status: 200, data: {} }),
  );
  const w = mount(SettlementPlansPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request, requestResult }),
      adminClass: "super",
      ownSession: true,
    },
  });
  const click = async (text: string) => {
    await w
      .findAll("button")
      .find((b) => b.text() === text)!
      .trigger("click");
    await flushPromises();
  };
  await flushPromises();
  await click("查看");
  await click("核对到期扫描");
  await click("确认扫描当前到期时段");
  expect(requestResult).toHaveBeenCalledTimes(1);
  await click("紧急撤销原授权");
  expect(w.text()).toContain("确认紧急撤销");
  await click("确认紧急撤销");
  expect(requestResult).toHaveBeenCalledTimes(2);
  expect(requestResult.mock.calls[1]?.[0]).toBe(
    "/settlement-plan-authorizations/8/revoke",
  );
  w.unmount();
});
it("does not describe a malformed HTTP200 scan response as an accepted original run", async () => {
  const request = vi.fn((path: string) =>
    Promise.resolve(
      path === "/settlement-plans/4"
        ? plan
        : { items: [plan], next_cursor: "" },
    ),
  );
  const requestResult = vi.fn(() =>
    Promise.resolve({
      status: 200,
      data: { id: "9", plan_id: "4", state: "completed" },
    }),
  );
  const w = mount(SettlementPlansPage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request, requestResult }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  for (const name of ["查看", "核对到期扫描", "确认扫描当前到期时段"]) {
    await w
      .findAll("button")
      .find((b) => b.text() === name)!
      .trigger("click");
    await flushPromises();
  }
  expect(w.text()).not.toContain("扫描已受理");
  expect(w.find('[role="alert"]').exists()).toBe(true);
  w.unmount();
});
it("does not restart a merchant read after a late save resolves after unmount", async () => {
  const prefs = {
    merchant_id: "2",
    revision: "0",
    enabled: false,
    recipient_config_id: "0",
    recipient: {
      state: "configured",
      config_id: "12",
      type: "alipay",
      app_id: "",
      account_masked: "***a",
      name_masked: "***",
    },
  };
  let complete: () => void = () => {};
  const request = vi.fn((_path: string, options?: { method?: string }) =>
    options?.method === "PUT"
      ? new Promise<void>((r) => (complete = r))
      : Promise.resolve(prefs),
  );
  const w = mount(SettlementSchedulePage, {
    props: {
      client: Object.assign(new ApiClient("merchant"), { request }),
      ownSession: true,
    },
  });
  await flushPromises();
  await w.find('input[type="checkbox"]').setValue(true);
  await w
    .findAll("button")
    .find((b) => b.text() === "核对参与设置")!
    .trigger("click");
  await w
    .findAll("button")
    .find((b) => b.text() === "确认保存参与设置")!
    .trigger("click");
  expect(request).toHaveBeenCalledTimes(2);
  w.unmount();
  complete();
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
});
