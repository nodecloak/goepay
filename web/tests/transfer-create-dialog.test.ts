import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { ApiClient } from "../src/api";
import TransferPage from "../src/components/TransferPage.vue";
import { defaultTransferNo, transferCost } from "../src/transfers";

// 申请付款 opens the original console's 转账付款 dialog: type tabs, a label
// column with the original wording, the 交易号 notice, and 批量转账 / 立即转账
// in the footer. GOEPAY's checks and its 核对付款 step stay in place.

const caps = (over: Record<string, unknown> = {}) => ({
  writes_enabled: true,
  platform_enabled: true,
  merchant_enabled: true,
  transfer_rate: "3",
  available_money: "10.00",
  ledger_available_money: "10.00",
  today_blocked_money: "0.00",
  settle_type: 0,
  minimum_money: "1.00",
  maximum_money: "200.00",
  channels: [{ id: "42", types: ["alipay", "wxpay"] }],
  default_title: "",
  default_remark: "",
  ...over,
});
const created = {
  id: "7",
  merchant_id: "1000",
  amount: "100",
  status: "unknown",
  type: "alipay",
};

function setup(area: "merchant" | "admin", capabilities = caps()) {
  const client = new ApiClient(area);
  client.csrfToken = "t";
  const paths: string[] = [];
  const posts: unknown[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path: string) => {
    paths.push(path);
    if (path.startsWith("/transfers/capabilities"))
      return capabilities as never;
    if (path === "/transfers/7")
      return { transfer: created, recipient: {} } as never;
    return { items: [], total: 0 } as never;
  });
  vi.spyOn(client, "requestResult").mockImplementation(
    async (_path: string, opts?: { body?: unknown }) => {
      posts.push(opts?.body);
      return { status: 202, data: created } as never;
    },
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:rest(.*)*", component: { render: () => null } }],
  });
  const w = mount(TransferPage, {
    props: { client, area, selfID: "1000" },
    global: { plugins: [router] },
    attachTo: document.body,
  });
  return { w, paths, posts };
}

const dialog = () =>
  document.body.querySelector<HTMLElement>('.pa-modal[role="dialog"]')!;
const labels = () =>
  Array.from(
    dialog().querySelectorAll(".transfer-form > .pa-field > .pa-label"),
  ).map((l) => l.textContent?.trim());
const input = (name: string) =>
  dialog().querySelector<HTMLInputElement>(`input[aria-label="${name}"]`)!;
async function type(name: string, value: string) {
  const el = input(name);
  el.value = value;
  el.dispatchEvent(new Event("input"));
  await flushPromises();
}
async function open(w: ReturnType<typeof setup>["w"]) {
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text().includes("申请付款"))!
    .trigger("click");
  await flushPromises();
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

it("opens as 转账付款 with type tabs, the original field wording and the 交易号 notice", async () => {
  const { w } = setup("merchant");
  await open(w);
  expect(dialog().getAttribute("aria-label")).toBe("转账付款");
  const tabs = Array.from(dialog().querySelectorAll('[role="tab"]'));
  expect(tabs.map((t) => t.textContent?.trim())).toEqual([
    "支付宝",
    "微信",
    "银行卡",
  ]);
  expect(tabs[0]!.getAttribute("aria-selected")).toBe("true");
  // No bank channel for this merchant: the 银行卡 tab is shown but disabled.
  expect((tabs[2] as HTMLButtonElement).disabled).toBe(true);
  expect(labels()).toEqual([
    "通道选择",
    "交易号",
    "支付宝账号",
    "支付宝姓名",
    "转账金额",
    "转账标题",
  ]);
  expect(input("交易号").value).toMatch(/^\d{19}$/);
  expect(input("支付宝账号").placeholder).toBe(
    "支付宝登录账号或支付宝UID或支付宝Openid",
  );
  expect(input("支付宝姓名").placeholder).toBe("不填写则不校验真实姓名");
  expect(input("转账金额").placeholder).toBe("RMB/元");
  expect(input("转账标题").placeholder).toBe("可留空");
  expect(dialog().querySelector(".transfer-tip")?.textContent).toContain(
    "交易号可以防止重复转账，同一个交易号只能提交同一次转账。",
  );
  const foot = dialog().querySelector(".pa-modal-foot")!;
  expect(
    Array.from(foot.querySelectorAll("button")).map((b) =>
      b.textContent?.trim(),
    ),
  ).toEqual(["立即转账"]);
  expect(foot.querySelector("a")?.textContent?.trim()).toBe("批量转账");
  expect(foot.querySelector("a")?.getAttribute("href")).toBe(
    "/merchant/transfer/batches",
  );
  w.unmount();
});

it("switches wording with the tab and shows what the merchant pays", async () => {
  const { w } = setup("merchant");
  await open(w);
  const wx = dialog().querySelectorAll<HTMLButtonElement>('[role="tab"]')[1]!;
  wx.click();
  await flushPromises();
  expect(wx.getAttribute("aria-selected")).toBe("true");
  expect(labels()).toEqual([
    "通道选择",
    "交易号",
    "Openid",
    "真实姓名",
    "转账金额",
    "转账备注",
  ]);
  expect(input("Openid").placeholder).toBe("只能填写微信Openid");
  // WeChat needs a remark and this merchant has no default one.
  expect(input("转账备注").placeholder).toBe("微信付款必须填写");
  expect(
    dialog()
      .querySelector(".transfer-form > .pa-field:last-child .pa-label")
      ?.classList.contains("pa-label--req"),
  ).toBe(true);
  await type("转账金额", "100");
  expect(input("需支付金额").value).toBe("103.00");
  expect(dialog().textContent).toContain(
    "可出账 10.00 元 · 单笔 1.00–200.00 元 · 手续费率 3%",
  );
  w.unmount();
});

it("the policy line leaves out single-transfer limits that are not set", async () => {
  const { w } = setup(
    "merchant",
    caps({ minimum_money: "0.00", maximum_money: "0.00", transfer_rate: "0" }),
  );
  await open(w);
  const hint = dialog().querySelector(".transfer-note")?.textContent?.trim();
  expect(hint).toBe("可出账 10.00 元 · 手续费率 0%");
  w.unmount();
});

it("立即转账 goes through 核对付款 and posts the same body as before", async () => {
  const { w, posts } = setup("merchant");
  await open(w);
  const no = input("交易号").value;
  await type("支付宝账号", "payee@example.test");
  await type("转账金额", "1.00");
  dialog()
    .querySelector<HTMLButtonElement>('.pa-modal-foot button[type="submit"]')!
    .click();
  await flushPromises();
  expect(dialog().getAttribute("aria-label")).toBe("核对付款");
  const text = dialog().textContent || "";
  expect(text).toContain("支付宝账号payee@example.test");
  expect(text).toContain("需支付金额（元）1.03（含手续费 0.03）");
  expect(text).toContain("交易号" + no);
  expect(posts).toHaveLength(0);
  Array.from(dialog().querySelectorAll("button"))
    .find((b) => b.textContent?.trim() === "确认提交付款")!
    .click();
  await flushPromises();
  expect(posts).toEqual([
    {
      channel_id: "42",
      type: "alipay",
      account: "payee@example.test",
      name: "",
      amount: "1.00",
      remark: "",
      external_no: no,
    },
  ]);
  w.unmount();
});

it("keeps 立即转账 disabled, without a spinner, when payouts are off", async () => {
  const { w } = setup(
    "merchant",
    caps({ writes_enabled: false, platform_enabled: false }),
  );
  await open(w);
  expect(dialog().textContent).toContain("平台付款功能未启用");
  const submit = dialog().querySelector<HTMLButtonElement>(
    '.pa-modal-foot button[type="submit"]',
  )!;
  expect(submit.disabled).toBe(true);
  expect(submit.classList.contains("pa-btn--loading")).toBe(false);
  w.unmount();
});

it("admin reads the paying merchant's permissions before a channel can be chosen", async () => {
  const { w, paths } = setup("admin");
  await open(w);
  expect(labels()[0]).toBe("付款商户");
  const channel = dialog().querySelector<HTMLButtonElement>(
    '.pa-select-trigger[aria-label="通道选择"]',
  )!;
  expect(channel.disabled).toBe(true);
  expect(channel.textContent).toContain("请先读取付款权限");
  await type("付款商户ID", "1000");
  Array.from(dialog().querySelectorAll("button"))
    .find((b) => b.textContent?.trim() === "读取付款权限")!
    .click();
  await flushPromises();
  expect(paths).toContain("/transfers/capabilities?merchant_id=1000");
  expect(channel.disabled).toBe(false);
  expect(channel.textContent).toContain("通道 42");
  w.unmount();
});

it("transferCost adds the fee the way the server rounds it", () => {
  expect(transferCost("1.00", "3")).toEqual({ cost: "1.03", fee: "0.03" });
  expect(transferCost("100", "0.6")).toEqual({ cost: "100.60", fee: "0.60" });
  // Half a cent rounds up, less than half rounds down.
  expect(transferCost("0.50", "1")).toEqual({ cost: "0.51", fee: "0.01" });
  expect(transferCost("0.01", "3")).toEqual({ cost: "0.01", fee: "0.00" });
  expect(transferCost("1.00", undefined)).toEqual({
    cost: "1.00",
    fee: "0.00",
  });
  expect(transferCost("", "3")).toBeUndefined();
  expect(transferCost("1.005", "3")).toBeUndefined();
  expect(transferCost("1.00", "100.01")).toBeUndefined();
});

it("defaultTransferNo is the original 19-digit date + random number", () => {
  const at = new Date(2026, 9, 29, 23, 23, 44);
  expect(defaultTransferNo(at, 0)).toBe("2026102923234411111");
  expect(defaultTransferNo(at, 0.999999)).toBe("2026102923234499999");
});
