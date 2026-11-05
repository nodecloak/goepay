import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";
import CorePaymentRisk from "../src/components/CorePaymentRisk.vue";
import { coreRiskFields, readCoreRiskPolicy } from "../src/core-payment-risk";
it("opens the original executable risk settings from the real settings tab", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic-risk177";
  const request = vi.fn(async (path: string) => {
    if (path.endsWith("/schema"))
      return {
        fields: [{ name: "key", options: ["site_name"] }],
        permissions: { create: true, update: true },
      };
    if (path.startsWith("/resources/settings")) return { items: [], total: 0 };
    throw Error("risk data not requested correctly");
  });
  c.request = request as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client: c, ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  await w.get("#settings-tab-risk").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("核心付款风控");
  expect(request.mock.calls.some(([path]) => path === "/payment-risk")).toBe(
    true,
  );
  w.unmount();
});
function policy(revision = "0") {
  return {
    configured: revision !== "0",
    revision,
    fingerprint: revision === "0" ? "a".repeat(64) : "b".repeat(64),
    geo_configured: false,
    settings: Object.fromEntries(
      coreRiskFields.map((f) => [
        f.key,
        f.kind === "flag" || f.kind === "scan" ? "0" : "",
      ]),
    ),
  };
}
function client(request: ReturnType<typeof vi.fn>) {
  const c = new ApiClient("admin");
  c.csrfToken = "risk177-original-session";
  c.request = request as ApiClient["request"];
  return c;
}
it("validates the complete DTO and preserves revisions above JS safe integer", () => {
  expect(readCoreRiskPolicy(policy("9007199254740993")).revision).toBe(
    "9007199254740993",
  );
  expect(() =>
    readCoreRiskPolicy({ ...policy(), revision: 9007199254740993 }),
  ).toThrow();
  expect(() =>
    readCoreRiskPolicy({ ...policy(), proof_hash: "private" }),
  ).toThrow();
  expect(() =>
    readCoreRiskPolicy({
      ...policy(),
      settings: { ...policy().settings, secret: "bad" },
    }),
  ).toThrow();
});
it("cancel never writes; same-instance CSRF replacement cannot send old draft", async () => {
  const request = vi.fn(async (path: string) =>
      path === "/payment-risk/actions" ? { items: [], total: 0 } : policy(),
    ),
    c = client(request),
    w = mount(CorePaymentRisk, {
      props: { client: c, allowed: true },
      attachTo: document.body,
    });
  await flushPromises();
  await w.get("#risk177-pay_iplimit").setValue("3");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  await w
    .findAll("button")
    .find((x) => x.text() === "取消")!
    .trigger("click");
  expect(
    request.mock.calls.filter(([path]) => path === "/payment-risk"),
  ).toHaveLength(1);
  await w.get("form").trigger("submit");
  c.csrfToken = "risk177-replaced";
  await w
    .findAll("button")
    .find((x) => x.text() === "确认执行")!
    .trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([path]) => path === "/payment-risk"),
  ).toHaveLength(1);
  expect(w.text()).not.toContain("已保存");
  expect(w.find("#risk177-pay_iplimit").exists()).toBe(false);
  w.unmount();
});
it("late GET does not restore fields after client changes", async () => {
  let resolve!: (v: unknown) => void;
  const c = client(vi.fn().mockReturnValue(new Promise((r) => (resolve = r)))),
    w = mount(CorePaymentRisk, { props: { client: c, allowed: true } });
  await w.setProps({ client: client(vi.fn().mockResolvedValue(policy("1"))) });
  resolve(policy());
  await flushPromises();
  expect(w.find("#risk177-pay_iplimit").exists()).toBe(false);
  expect(w.text()).not.toContain("尚未配置，默认关闭");
  w.unmount();
});
it("an in-flight policy write keeps real settings tabs and outer refresh disabled", async () => {
  let resolve!: (v: unknown) => void;
  const request = vi.fn(async (path: string, options?: { method?: string }) => {
    if (path === "/payment-risk/actions") return { items: [], total: 0 };
    if (path.endsWith("/schema"))
      return {
        fields: [{ name: "key", options: ["site_name"] }],
        permissions: { create: true, update: true },
      };
    if (path.startsWith("/resources/settings")) return { items: [], total: 0 };
    if (options?.method === "PATCH") return new Promise((r) => (resolve = r));
    return policy();
  });
  const w = mount(SettingsPage, {
    props: { client: client(request), ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  await w.get("#settings-tab-risk").trigger("click");
  await flushPromises();
  await w.get("#risk177-pay_iplimit").setValue("3");
  await w.get(".core-risk form").trigger("submit");
  await w
    .findAll("button")
    .find((x) => x.text() === "确认执行")!
    .trigger("click");
  await flushPromises();
  expect(w.get("#settings-tab-site").attributes("disabled")).toBeDefined();
  expect(w.findAll("button").some((b) => b.text() === "刷新设置")).toBe(false);
  const next = policy("1");
  next.settings.pay_iplimit = "3";
  resolve(next);
  await flushPromises();
  expect(w.get("#settings-tab-site").attributes("disabled")).toBeUndefined();
  w.unmount();
});
it("a mismatched policy commit is uncertain and never reported saved", async () => {
  const request = vi.fn(async (path: string, options?: { method?: string }) =>
      path === "/payment-risk/actions"
        ? { items: [], total: 0 }
        : policy(options?.method === "PATCH" ? "1" : "0"),
    ),
    w = mount(CorePaymentRisk, {
      props: { client: client(request), allowed: true },
    });
  await flushPromises();
  await w.get("#risk177-pay_iplimit").setValue("3");
  await w.get("form").trigger("submit");
  await w
    .findAll("button")
    .find((x) => x.text() === "确认执行")!
    .trigger("click");
  await flushPromises();
  expect(w.text()).not.toContain("已保存");
  expect(w.text()).toContain("结果无法确认");
  expect(
    request.mock.calls.filter(([path]) => path === "/payment-risk"),
  ).toHaveLength(2);
  w.unmount();
});
it("an unknown manual check recovers by GET using its original key and never reruns", async () => {
  let key = "";
  const request = vi.fn(
    async (
      path: string,
      options?: { method?: string; body?: Record<string, unknown> },
    ) => {
      if (path === "/payment-risk/actions") return { items: [], total: 0 };
      if (options?.method === "POST") {
        key = String(options.body!.external_no);
        throw Error("synthetic lost response");
      }
      if (path.startsWith("/payment-risk/check-runs/"))
        return {
          external_no: key,
          policy_revision: "1",
          actions: 0,
          scanned: 0,
          at: "2026-10-26T10:00:00Z",
        };
      return policy("1");
    },
  );
  const w = mount(CorePaymentRisk, {
    props: { client: client(request), allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((x) => x.text() === "运行一次核心检查")!
    .trigger("click");
  await w
    .findAll("button")
    .find((x) => x.text() === "确认执行")!
    .trigger("click");
  await flushPromises();
  expect(key).toMatch(/^riskcheck-/);
  const recover = w
    .findAll("button")
    .find((x) => x.text() === "核对上次检查结果");
  expect(recover).toBeDefined();
  await recover!.trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(1);
  expect(
    request.mock.calls.some(
      ([path]) => path === "/payment-risk/check-runs/" + key,
    ),
  ).toBe(true);
  expect(w.text()).toContain("已核对原检查结果");
  w.unmount();
});
it("the current risk page actually reads its blocking actions", async () => {
  const request = vi.fn(async (path: string) =>
      path === "/payment-risk/actions" ? { items: [], total: 0 } : policy("1"),
    ),
    w = mount(CorePaymentRisk, {
      props: { client: client(request), allowed: true },
    });
  await flushPromises();
  expect(w.text()).toContain("风控阻断记录");
  expect(
    request.mock.calls.some(([path]) => path === "/payment-risk/actions"),
  ).toBe(true);
  w.unmount();
});
it("missing local geographic lookup cannot be enabled through confirmation", async () => {
  const request = vi.fn(async (path: string) =>
      path === "/payment-risk/actions" ? { items: [], total: 0 } : policy(),
    ),
    w = mount(CorePaymentRisk, {
      props: { client: client(request), allowed: true },
    });
  await flushPromises();
  await w.get("#risk177-pay_block_oversea").setValue("1");
  await w.get("form").trigger("submit");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).toContain("请先配置本地地址库");
  w.unmount();
});
