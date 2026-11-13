import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import CryptoChannelEditor from "../src/components/CryptoChannelEditor.vue";
import ParentChannelPage from "../src/components/ParentChannelPage.vue";

const row = {
  asset: "USDT",
  id: "42",
  name: "TRON 收款",
  pay_type: "usdt",
  plugin: "usdt_tron",
  configured: true,
  network: "tron",
  revision: "3",
  enabled: true,
  archived: false,
  runtime_enabled: false,
  fee_bps: 125,
  min_amount: "0.1",
  max_amount: "1000",
  day_limit: "2000",
  merchant_ids: ["123"],
  created_at: "2026-11-13T00:00:00Z",
  updated_at: "2026-11-13T00:00:00Z",
} as const;
let w: VueWrapper | undefined;
afterEach(() => {
  w?.unmount();
  w = undefined;
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});
it("renders USDT inside payment channels with only asset appropriate actions", async () => {
  const c = new ApiClient("admin");
  vi.spyOn(c, "request").mockImplementation(async (path) =>
    path.includes("templates")
      ? { items: [], total: 0 }
      : { items: [row], total: 1, page: 1, size: 20 },
  );
  w = mount(ParentChannelPage, { props: { client: c, allowed: true } });
  await flushPromises();
  expect(w.text()).toContain("TRON 收款");
  expect(w.text()).toContain("主网收款关闭");
  expect(w.text()).toContain("新增 USDT 通道");
  const tr = w.get("tbody tr");
  expect(tr.text()).not.toContain("配置密钥");
  expect(tr.text()).not.toContain("测试");
  expect(tr.find("input[type=checkbox]").attributes("disabled")).toBeDefined();
  expect(tr.get("a").attributes("href")).toBe(
    "/admin/trade/order?type=usdt&channel_id=42",
  );
});
it("edits explicit fee and grants without allowing network change or enabling runtime", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "original";
  const request = vi
    .spyOn(c, "request")
    .mockResolvedValue({ ...row, revision: "4" });
  w = mount(CryptoChannelEditor, {
    props: { client: c, row: { ...row, merchant_ids: [...row.merchant_ids] } },
    global: { stubs: { teleport: true } },
  });
  expect(w.get('select[name="network"]').attributes("disabled")).toBeDefined();
  expect(w.text()).toContain("主网收款关闭");
  await w.get('input[name="fee"]').setValue("0.75");
  await w.get('textarea[name="merchants"]').setValue("123,456");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/payment-channels/crypto/42",
    expect.objectContaining({
      method: "PATCH",
      body: expect.objectContaining({
        fee_bps: 75,
        merchant_ids: ["123", "456"],
        expected_revision: "3",
        network: "tron",
      }),
    }),
  );
  expect(w.emitted("saved")).toHaveLength(1);
});
it("locks an uncertain save and never automatically repeats it", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "original";
  const request = vi
    .spyOn(c, "request")
    .mockRejectedValue(new Error("connection lost"));
  w = mount(CryptoChannelEditor, {
    props: { client: c, row: { ...row, merchant_ids: [...row.merchant_ids] } },
    global: { stubs: { teleport: true } },
  });
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.text()).toContain("保存结果尚未确认");
  expect(w.get('button[type="submit"]').attributes("disabled")).toBeDefined();
  await w.get("form").trigger("submit");
  expect(request).toHaveBeenCalledTimes(1);
});
it("opens archived channels read-only and closes without sending an update", async () => {
  const c = new ApiClient("admin");
  const request = vi.spyOn(c, "request");
  w = mount(CryptoChannelEditor, {
    props: {
      client: c,
      row: {
        ...row,
        enabled: false,
        archived: true,
        merchant_ids: [...row.merchant_ids],
      },
    },
    global: { stubs: { teleport: true } },
  });
  expect(w.get('[role="dialog"]').attributes("aria-label")).toBe(
    "USDT 通道详情",
  );
  expect(w.get("fieldset").attributes("disabled")).toBeDefined();
  expect(w.text()).toContain("已归档，只能查看");
  expect(w.get('button[type="submit"]').text()).toBe("关闭");
  await w.get("form").trigger("submit");
  expect(request).not.toHaveBeenCalled();
  expect(w.emitted("close")).toHaveLength(1);
});
