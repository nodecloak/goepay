// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { nextTick, ref } from "vue";
import OrderDetails from "../src/components/OrderDetails.vue";
import LegacyCombinedOrders204 from "../src/components/LegacyCombinedOrders204.vue";
import { ApiClient } from "../src/api";

const mounted204: VueWrapper[] = [];
afterEach(() => {
  mounted204.splice(0).forEach((wrapper) => wrapper.unmount());
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function children204(parentSettle = 1) {
  return {
    trade_no: "PARENT-204",
    read_only: true,
    verified_payment_source: false,
    source: "epay_pro",
    export_id: "synthetic-original-export204",
    exported_at: "2026-10-27T01:02:03Z",
    parent_settle: parentSettle,
    charged_money: "9007199254741006.02",
    children_money: "9007199254741006.02",
    amounts_match: true,
    items: [
      {
        sub_trade_no: "9007199254740993123",
        api_trade_no: "API/<script>204</script>",
        money: "9007199254740993.02",
        refundmoney: "9007199254740993.01",
        status: 2,
        settle: 1,
      },
      {
        sub_trade_no: "CHILD-NULL-204",
        api_trade_no: "",
        money: "2.00",
        refundmoney: null,
        status: 2,
        settle: 0,
      },
      {
        sub_trade_no: "CHILD-PAID-204",
        api_trade_no: "API-PAID-204",
        money: "3.00",
        refundmoney: "0.01",
        status: 1,
        settle: 0,
      },
      {
        sub_trade_no: "CHILD-FULL-204",
        api_trade_no: "API-FULL-204",
        money: "4.00",
        refundmoney: "4.00",
        status: 2,
        settle: 1,
      },
      {
        sub_trade_no: "CHILD-ZERO-204",
        api_trade_no: "API-ZERO-204",
        money: "1.00",
        refundmoney: "0.00",
        status: 2,
        settle: 0,
      },
      {
        sub_trade_no: "CHILD-UNPAID-204",
        api_trade_no: "API-UNPAID-204",
        money: "1.00",
        refundmoney: null,
        status: 0,
        settle: 0,
      },
      {
        sub_trade_no: "CHILD-FROZEN-204",
        api_trade_no: "API-FROZEN-204",
        money: "1.00",
        refundmoney: null,
        status: 3,
        settle: 0,
      },
      {
        sub_trade_no: "CHILD-AUTH-204",
        api_trade_no: "API-AUTH-204",
        money: "1.00",
        refundmoney: null,
        status: 4,
        settle: 0,
      },
    ],
  };
}

it.each([
  {
    name: "mutable response",
    change: (v: ReturnType<typeof children204>) => {
      v.read_only = false;
    },
  },
  {
    name: "promoted payment source",
    change: (v: ReturnType<typeof children204>) => {
      v.verified_payment_source = true;
    },
  },
  {
    name: "wrong parent",
    change: (v: ReturnType<typeof children204>) => {
      v.trade_no = "OTHER-204";
    },
  },
  {
    name: "unconfirmed total",
    change: (v: ReturnType<typeof children204>) => {
      v.amounts_match = false;
    },
  },
  {
    name: "mismatched total",
    change: (v: ReturnType<typeof children204>) => {
      v.children_money = "9007199254741006.03";
    },
  },
  {
    name: "floating exponent",
    change: (v: ReturnType<typeof children204>) => {
      v.items[0].money = "1e2";
    },
  },
  {
    name: "numeric refund instead of null",
    change: (v: ReturnType<typeof children204>) => {
      Object.assign(v.items[1], { refundmoney: 0 });
    },
  },
  {
    name: "unsupported status",
    change: (v: ReturnType<typeof children204>) => {
      v.items[0].status = 5;
    },
  },
  {
    name: "unsupported settlement",
    change: (v: ReturnType<typeof children204>) => {
      v.items[0].settle = 2;
    },
  },
  {
    name: "duplicate child",
    change: (v: ReturnType<typeof children204>) => {
      v.items[1].sub_trade_no = v.items[0].sub_trade_no;
    },
  },
])(
  "does not render unverifiable historical children: $name",
  async ({ change }) => {
    const value = children204();
    change(value);
    const f = setup204({ reply: async () => response204(value) });
    const modal = await open204(f.wrapper);
    expect(modal.find('[role="alert"]').exists()).toBe(true);
    expect(modal.findAll("tbody tr")).toHaveLength(0);
    expect(f.calls.every((call) => call.method === "GET")).toBe(true);
  },
);

it.each(["pending", "loaded"])(
  "clears a %s child projection when the parent merchant subject changes",
  async (phase) => {
    let resolve!: (response: Response) => void;
    const reply = new Promise<Response>((done) => {
      resolve = done;
    });
    let signal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        signal = init?.signal || undefined;
        return phase === "pending" ? reply : response204(children204());
      }),
    );
    const client = ref(new ApiClient("merchant")).value;
    client.csrfToken = "subject-context204";
    const wrapper = mount(LegacyCombinedOrders204, {
      props: {
        client,
        tradeNo: "PARENT-204",
        merchantID: "1",
        contextStamp: 1,
      },
      global: { stubs: { Teleport: true } },
    });
    mounted204.push(wrapper);
    await flushPromises();
    if (phase === "loaded") expect(wrapper.findAll("tbody tr")).toHaveLength(8);
    await wrapper.setProps({ merchantID: "2" });
    expect(signal?.aborted).toBe(true);
    expect(wrapper.findAll("tbody tr")).toHaveLength(0);
    expect(wrapper.emitted("close")).toHaveLength(1);
    resolve(response204(children204()));
    await flushPromises();
    expect(wrapper.findAll("tbody tr")).toHaveLength(0);
  },
);
function response204(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({
      code: status === 200 ? 0 : -1,
      message: status === 200 ? "" : "历史子订单读取失败204",
      data,
    }),
    { status, headers: { "Content-Type": "application/json" } },
  );
}
function setup204(
  options: {
    area?: "admin" | "merchant";
    ownSession?: boolean;
    combine?: unknown;
    allowed?: unknown;
    reply?: () => Promise<Response>;
  } = {},
) {
  const calls: { path: string; method: string }[] = [];
  const signals: AbortSignal[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input),
        method = init?.method || "GET";
      calls.push({ path, method });
      if (path.endsWith("/suborders")) {
        if (init?.signal) signals.push(init.signal);
        return options.reply ? options.reply() : response204(children204());
      }
      if (method !== "GET")
        throw new Error(
          "historical combined-order read invoked a financial mutation",
        );
      return response204({
        order: {
          trade_no: "PARENT-204",
          merchant_id: "9007199254740993",
          amount: "500",
          status: "paid",
          combine: options.combine ?? 1,
        },
        can_view_suborders: options.allowed ?? true,
        can_resend_notification: false,
        can_return_notification: false,
      });
    }),
  );
  const client = ref(new ApiClient(options.area || "merchant")).value;
  client.csrfToken = "csrf-legacy204";
  const wrapper = mount(OrderDetails, {
    props: {
      client,
      tradeNo: "PARENT-204",
      ownSession: options.ownSession ?? true,
    },
    attachTo: document.body,
    global: {
      stubs: {
        Teleport: true,
        OrderIncomePanel: true,
        OrderPrintingPanel: true,
        PreauthorizationObservation: true,
        ScanRecoveryPanel: true,
        NotificationDeliveryPanel: true,
      },
    },
  });
  mounted204.push(wrapper);
  return { wrapper, client, calls, signals };
}
function button204(wrapper: VueWrapper) {
  return wrapper
    .findAll("button")
    .find((button) => button.text() === "查看子订单");
}
async function open204(wrapper: VueWrapper) {
  await flushPromises();
  expect(
    button204(wrapper),
    "verified historical relation must expose the real order-detail read action",
  ).toBeDefined();
  await button204(wrapper)!.trigger("click");
  await flushPromises();
  return wrapper.find('[role="dialog"][aria-label="合单支付 - 子订单列表"]');
}

it.each(["merchant", "admin"] as const)(
  "%s detail lazily reads verified historical children and preserves exact yuan values",
  async (area) => {
    const f = setup204({ area });
    await flushPromises();
    expect(f.calls).toEqual([
      { path: `/console/v1/${area}/orders/PARENT-204`, method: "GET" },
    ]);
    const modal = await open204(f.wrapper);
    expect(modal.exists()).toBe(true);
    expect(f.calls).toEqual([
      { path: `/console/v1/${area}/orders/PARENT-204`, method: "GET" },
      {
        path: `/console/v1/${area}/orders/PARENT-204/suborders`,
        method: "GET",
      },
    ]);
    expect(modal.text()).toContain("仅供查询");
    expect(modal.findAll("thead th").map((cell) => cell.text())).toEqual([
      "系统订单号",
      "接口订单号",
      "订单金额",
      "订单状态",
      "结算状态",
    ]);
    const rows = modal.findAll("tbody tr");
    expect(rows).toHaveLength(8);
    expect(rows[0].text()).toContain("9007199254740993123");
    expect(rows[0].text()).toContain("¥9007199254740993.02");
    expect(rows[0].text()).toContain("已退款(9007199254740993.01)");
    expect(rows[0].text()).toContain("API/<script>204</script>");
    expect(modal.find("script").exists()).toBe(false);
    expect(rows[1].text()).toContain("已退款");
    expect(rows[1].text()).not.toContain("未退款");
    expect(rows[1].text()).not.toContain("已退款(0.00)");
    expect(rows[2].text()).toContain("已支付");
    expect(rows[2].text()).not.toContain("(0.01)");
    expect(rows[3].text()).not.toContain("(4.00)");
    expect(rows[4].text()).not.toContain("(0.00)");
    expect(rows[5].text()).toContain("未支付");
    expect(rows[6].text()).toContain("已冻结");
    expect(rows[7].text()).toContain("预授权");
    expect(modal.text()).toContain("9007199254741006.02");
    expect(
      modal
        .findAll("button")
        .map((b) => b.text())
        .some((text) => /退款|结算|扣款/.test(text)),
    ).toBe(false);
    expect(f.calls.every((call) => call.method === "GET")).toBe(true);
  },
);

it.each([
  { combine: 1, allowed: false },
  { combine: 1, allowed: "true" },
  { combine: 0, allowed: true },
  { combine: "1", allowed: true },
])(
  "does not infer child-read authority from unsealed or contradictory facts %j",
  async (options) => {
    const f = setup204(options);
    await flushPromises();
    expect(button204(f.wrapper)).toBeUndefined();
    expect(f.calls.some((call) => call.path.endsWith("/suborders"))).toBe(
      false,
    );
  },
);

it("permits a delegated read granted by the parent detail and hides settlement when parent_settle is zero", async () => {
  const f = setup204({
    ownSession: false,
    reply: async () => response204(children204(0)),
  });
  const modal = await open204(f.wrapper);
  expect(modal.exists()).toBe(true);
  expect(modal.findAll("thead th").map((cell) => cell.text())).not.toContain(
    "结算状态",
  );
  expect(
    f.calls.filter((call) => call.path.endsWith("/suborders")),
  ).toHaveLength(1);
});

it("a failed historical read can be retried without a mutation or stale child list", async () => {
  let attempts = 0;
  const f = setup204({
    reply: async () =>
      ++attempts === 1 ? response204(null, 503) : response204(children204()),
  });
  let modal = await open204(f.wrapper);
  expect(modal.find('[role="alert"]').exists()).toBe(true);
  expect(modal.findAll("tbody tr")).toHaveLength(0);
  await modal
    .findAll("button")
    .find((button) => button.text() === "重新读取子订单")!
    .trigger("click");
  await flushPromises();
  modal = f.wrapper.find('[role="dialog"][aria-label="合单支付 - 子订单列表"]');
  expect(modal.findAll("tbody tr")).toHaveLength(8);
  expect(attempts).toBe(2);
  expect(f.calls.every((call) => call.method === "GET")).toBe(true);
});

it.each([
  "trade",
  "area",
  "client",
  "csrf",
  "session-context",
  "close",
  "unmount",
])("aborts and ignores children after %s changes", async (change) => {
  let resolve!: (response: Response) => void;
  const pending = new Promise<Response>((done) => {
    resolve = done;
  });
  const f = setup204({ reply: () => pending });
  const modal = await open204(f.wrapper);
  expect(modal.exists()).toBe(true);
  expect(f.signals).toHaveLength(1);
  if (change === "trade")
    await f.wrapper.setProps({ tradeNo: "OTHER-PARENT-204" });
  if (change === "area") {
    f.client.area = "admin";
    await nextTick();
  }
  if (change === "client")
    await f.wrapper.setProps({
      client: Object.assign(new ApiClient("merchant"), {
        csrfToken: "replacement-204",
      }),
    });
  if (change === "csrf") {
    f.client.csrfToken = "replacement-204";
    await nextTick();
  }
  if (change === "session-context")
    await f.wrapper.setProps({ ownSession: false });
  if (change === "close")
    await modal
      .findAll("button")
      .find((button) => button.text() === "关闭子订单列表")!
      .trigger("click");
  if (change === "unmount") {
    f.wrapper.unmount();
    mounted204.splice(mounted204.indexOf(f.wrapper), 1);
  }
  expect(f.signals[0].aborted).toBe(true);
  resolve(response204(children204()));
  await flushPromises();
  expect(document.body.textContent).not.toContain("CHILD-NULL-204");
  expect(f.calls.every((call) => call.method === "GET")).toBe(true);
});
