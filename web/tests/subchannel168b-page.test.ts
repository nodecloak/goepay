import { afterEach, describe, it, expect, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { nextTick } from "vue";
import SubchannelPage from "../src/components/SubchannelPage.vue";
import ActionDropdown from "../src/components/ActionDropdown.vue";
import { ApiClient } from "../src/api";
import fixture from "../e2e/subchannel168b-fixture.json";
let wrapper: VueWrapper | undefined;
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
const row = () => structuredClone(fixture.merchant.items[0]);
const buttons = () => [
  ...document.querySelectorAll<HTMLButtonElement>("button"),
];
const click = async (label: string) => {
  const button = buttons().find((b) => b.textContent?.trim() === label);
  expect(button, "button " + label).toBeTruthy();
  button!.focus();
  button!.click();
  await flushPromises();
  return button!;
};
function setup(extra?: (path: string, options: any) => unknown) {
  const client = new ApiClient("merchant");
  client.csrfToken = "synthetic168b";
  const writes: { path: string; body: unknown }[] = [];
  client.request = vi.fn(async (path: string, options: any = {}) => {
    if (options.method && options.method !== "GET")
      writes.push({ path, body: options.body });
    const custom = extra?.(path, options);
    if (custom !== undefined) return await custom;
    if (path.startsWith("/payment-subchannels?"))
      return structuredClone(fixture.merchant);
    if (path === "/payment-subchannels/policy")
      return structuredClone(fixture.policy);
    if (/^\/payment-subchannels\/[1-9]\d*$/.test(path)) return row();
    throw Error("unhandled local " + path);
  }) as typeof client.request;
  vi.stubGlobal("crypto", {
    randomUUID: () => "12345678-1234-1234-1234-123456789012",
  });
  wrapper = mount(SubchannelPage, {
    props: { client, merchantId: "1", ownSession: true },
    attachTo: document.body,
    global: {
      stubs: {
        RouterLink: { props: ["to"], template: '<a :href="to"><slot /></a>' },
      },
    },
  });
  return { client, writes };
}
describe("subchannel168b protected merchant controls", () => {
  it("shows original columns, exact IDs and one owner metadata read, with no manual account creation", async () => {
    const { client } = setup();
    await flushPromises();
    expect(wrapper!.text()).toContain("9007199254740993");
    expect(wrapper!.findAll("th").map((n) => n.text())).toEqual([
      "编号",
      "支付方式",
      "名称",
      "费率",
      "今日",
      "昨日",
      "成功率",
      "状态",
      "权重",
      "操作",
    ]);
    expect(wrapper!.text()).toContain("0.50%");
    expect(wrapper!.text()).not.toContain("新增子通道");
    expect(
      (client.request as any).mock.calls.filter(([p]: string[]) =>
        p.startsWith("/payment-subchannels?"),
      ),
    ).toHaveLength(1);
    expect(wrapper!.text()).not.toContain("synthetic-only-secret");
  });
  it("cancels roll settings without a write and restores the opener focus", async () => {
    const { writes } = setup();
    await flushPromises();
    const opener = await click("轮询设置");
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();
    // 轮询方式 is now a kit KitSelect; its trigger shows the current choice
    // (rollType 2 = 权重轮询).
    expect(
      document
        .querySelector('[role="dialog"] .pa-select-trigger')
        ?.textContent?.trim(),
    ).toBe("权重轮询");
    await click("取消");
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(writes).toHaveLength(0);
    expect(document.activeElement).toBe(opener);
  });
  it("submits one exact owner-bound weight CAS without owner/parent/secret fields", async () => {
    const { writes } = setup();
    await flushPromises();
    wrapper!
      .find('button[aria-label="设置子通道 9007199254740993 权重"]')
      .element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();
    const input = document.querySelector(
      '[role="dialog"] input',
    ) as HTMLInputElement;
    expect(input).not.toBeNull();
    input.value = "12";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    await click("确认保存");
    expect(writes).toHaveLength(1);
    expect(writes[0]).toEqual({
      path: "/payment-subchannels/9007199254740993",
      body: { expected_fingerprint: row().fingerprint, weight: 12 },
    });
  });
  it("refuses a changed owner in the fresh detail response", async () => {
    const { writes } = setup((p) =>
      p === "/payment-subchannels/9007199254740993"
        ? { ...row(), merchant_id: "2" }
        : undefined,
    );
    await flushPromises();
    wrapper!
      .find('button[aria-label="设置子通道 9007199254740993 权重"]')
      .element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      "不属于当前商户",
    );
    const submit = buttons().find((b) => b.textContent?.trim() === "确认保存")!;
    expect(submit.disabled).toBe(true);
    document
      .querySelector("form.subchannel-form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flushPromises();
    expect(writes).toHaveLength(0);
  });
  it("confirms only once and blocks close/Escape while the payment test is in flight", async () => {
    let resolve: (v: unknown) => void = () => {};
    const deferred = new Promise((r) => (resolve = r));
    const { writes } = setup((p, o) =>
      p.endsWith("/test") && o.method === "POST" ? deferred : undefined,
    );
    await flushPromises();
    wrapper!.findAllComponents(ActionDropdown)[0]!.vm.$emit("action", "test");
    await flushPromises();
    expect(writes).toHaveLength(0);
    await click("确认生成一次支付测试");
    expect(writes).toHaveLength(1);
    const dialog = document.querySelector('[role="dialog"]')!;
    dialog.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        bubbles: true,
        cancelable: true,
      }),
    );
    await nextTick();
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();
    expect(
      buttons().find((b) => b.textContent?.trim() === "关闭并保留原订单")
        ?.disabled,
    ).toBe(true);
    resolve({
      trade_no: "synthetic168b-original",
      state: "unknown",
      url: "http://127.0.0.1/checkout/synthetic168b-original?expires=123&token=synthetic",
    });
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      "结果需要核对",
    );
    expect(writes).toHaveLength(1);
    expect(writes[0]?.body).toEqual({
      amount: "1.00",
      intent: "12345678123412341234123456789012",
    });
  });
  it("drops a late list response after the session context changes", async () => {
    let resolve: (v: unknown) => void = () => {};
    const deferred = new Promise((r) => (resolve = r));
    const { client } = setup((p) =>
      p.startsWith("/payment-subchannels?") ? deferred : undefined,
    );
    await flushPromises();
    await wrapper!.setProps({ ownSession: false });
    resolve(fixture.merchant);
    await flushPromises();
    expect(wrapper!.text()).not.toContain(row().name);
    expect(wrapper!.text()).toContain("本人商户会话");
    expect(
      (client.request as any).mock.calls.filter(([p]: string[]) =>
        p.startsWith("/payment-subchannels?"),
      ),
    ).toHaveLength(1);
  });
});
it("subchannel168b rejects a successful mutation DTO for another owner before reporting saved", async () => {
  const { writes } = setup((p, o) =>
    o.method === "PATCH"
      ? {
          ...row(),
          merchant_id: "2",
          revision: "2",
          weight: 12,
          fingerprint: "a".repeat(64),
        }
      : undefined,
  );
  await flushPromises();
  wrapper!
    .find('button[aria-label="设置子通道 9007199254740993 权重"]')
    .element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flushPromises();
  const input = document.querySelector(
    '[role="dialog"] input',
  ) as HTMLInputElement;
  input.value = "12";
  input.dispatchEvent(new Event("input", { bubbles: true }));
  await click("确认保存");
  expect(writes).toHaveLength(1);
  expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  expect(wrapper!.text()).not.toContain("设置已保存");
  expect(
    buttons().find((b) => b.textContent?.trim() === "确认保存")?.disabled,
  ).toBe(true);
});
it("subchannel168b clears an old admin secret form on raw same-client CSRF change before submit", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "old-stamp";
  const writes: any[] = [];
  client.request = vi.fn(async (p: string, o: any = {}) => {
    if (o.method && o.method !== "GET") {
      writes.push(o.body);
      return fixture.admin.items[0];
    }
    if (p.startsWith("/payment-subchannels?"))
      return structuredClone(fixture.admin);
    if (p === "/payment-subchannels/schemas")
      return structuredClone(fixture.schemas);
    throw Error(p);
  }) as any;
  wrapper = mount(SubchannelPage, {
    props: { client, merchantId: "168", ownSession: true, adminClass: "super" },
    attachTo: document.body,
    global: {
      stubs: {
        RouterLink: { props: ["to"], template: '<a :href="to"><slot/></a>' },
      },
    },
  });
  await flushPromises();
  expect(wrapper.findAll("thead th").map((h) => h.text())).toEqual([
    "ID",
    "支付方式",
    "子通道备注",
    "用户ID",
    "支付通道",
    "费率",
    "今日收款",
    "昨日收款",
    "今日成功率",
    "状态",
    "权重",
    "操作",
  ]);
  expect(
    wrapper.findAll(".subchannel-filter-fields label").map((l) => l.text()),
  ).toContain("用户ID");
  await click("新增子通道");
  const secret = document.querySelector(
    'textarea[aria-label="secret"]',
  ) as HTMLTextAreaElement;
  secret.value = "synthetic-secret-old-session";
  secret.dispatchEvent(new Event("input", { bubbles: true }));
  await flushPromises();
  client.csrfToken = "raw-new-stamp";
  document
    .querySelector("form.subchannel-form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await flushPromises();
  expect(writes).toHaveLength(0);
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.body.textContent).not.toContain(
    "synthetic-secret-old-session",
  );
  expect(wrapper!.text()).toContain("会话资料已变化");
});
it("subchannel168b raw CSRF change before copy drops the old protected URL without clipboard write", async () => {
  const { client } = setup((p) =>
    p.endsWith("/collection-link")
      ? {
          url: "https://synthetic.invalid/collect/1?type=alipay&subchannel_id=9007199254740993&token=old-protected-link",
        }
      : undefined,
  );
  await flushPromises();
  wrapper!.findComponent(ActionDropdown).vm.$emit("action", "link");
  await flushPromises();
  await click("读取收款链接");
  const copy = vi.fn();
  vi.stubGlobal("navigator", { clipboard: { writeText: copy } });
  client.csrfToken = "raw-new-stamp";
  await click("复制收款链接");
  expect(copy).not.toHaveBeenCalled();
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  expect(document.body.textContent).not.toContain("old-protected-link");
});
it("can stop an enabled original child when its parent is unavailable", async () => {
  setup((p) =>
    p.startsWith("/payment-subchannels?")
      ? {
          ...structuredClone(fixture.merchant),
          items: [
            {
              ...row(),
              parent_available: false,
              browser_test_available: false,
              unavailable_reason: "parent_unavailable",
            },
          ],
        }
      : p === "/payment-subchannels/9007199254740993"
        ? {
            ...row(),
            parent_available: false,
            browser_test_available: false,
            unavailable_reason: "parent_unavailable",
          }
        : undefined,
  );
  await flushPromises();
  // 状态 is now a kit KitSwitch; an enabled child with an unavailable parent can
  // still be stopped, so the switch stays enabled and toggling opens the confirm.
  const stop = document.querySelector(
    'input[role="switch"][aria-label="变更子通道 9007199254740993 状态"]',
  ) as HTMLInputElement;
  expect(stop).not.toBeNull();
  expect(stop.disabled).toBe(false);
  stop.checked = false;
  stop.dispatchEvent(new Event("change", { bubbles: true }));
  await flushPromises();
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
    "关闭",
  );
});
it("surfaces every original sub-channel row action (订单/支付测试/收款链接/通道设置/权重) with the 订单 link filtered by this sub-channel", async () => {
  setup();
  await flushPromises();
  const menu = wrapper!.findComponent(ActionDropdown);
  // A merchant row that supports a browser test offers all five Pro actions in
  // Pro's order; 订单 links to the order list filtered by this sub-channel,
  // 单笔限额设置 is the existing 通道设置 edit, 权重设置 is the weight field.
  const items = menu.props("items") as {
    id: string;
    label: string;
    href?: string;
  }[];
  expect(items.map((i) => i.id)).toEqual([
    "orders",
    "test",
    "link",
    "settings",
    "weight",
  ]);
  expect(items.map((i) => i.label)).toEqual([
    "支付订单",
    "支付测试",
    "收款链接",
    "单笔限额设置",
    "权重设置",
  ]);
  expect(items[0]!.href).toBe(
    "/merchant/trade/order?subchannel=9007199254740993",
  );
});
it("hides 支付测试 and 权重设置 when the product cannot browser-test and is not weighted", async () => {
  // A child whose product has no browser method and whose merchant roll is not
  // weighted (rollType != 2) cannot offer 支付测试 or 权重设置; everything
  // GOEPAY cannot do is simply not surfaced for that row rather than shown live.
  setup((p) =>
    p.startsWith("/payment-subchannels?")
      ? {
          ...structuredClone(fixture.merchant),
          items: [
            {
              ...row(),
              roll_type: 0,
              browser_test_available: false,
              methods: ["jsapi"],
            },
          ],
        }
      : undefined,
  );
  await flushPromises();
  const items = (
    wrapper!.findComponent(ActionDropdown).props("items") as { id: string }[]
  ).map((i) => i.id);
  expect(items).toEqual(["orders", "link", "settings"]);
  expect(items).not.toContain("test");
  expect(items).not.toContain("weight");
});
