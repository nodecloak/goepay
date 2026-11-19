import { afterEach, expect, it, vi } from "vitest";
import { computed, ref } from "vue";
import { webcrypto } from "node:crypto";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import { fundsPrincipal212, fundsScope212 } from "../src/funds-intent212";
import { CryptoIntentStore } from "../src/crypto-intent";
import CryptoFundsPage from "../src/components/CryptoFundsPage.vue";
import type { CryptoSection } from "../src/menu";
async function reveal(w: VueWrapper, form: string) {
  const sections: Record<string, CryptoSection> = {
    invoice: "invoices",
    withdrawal: "settlements",
    refund: "refunds",
    recipient: "recipients",
    address: "operations",
    "sweep-retry": "operations",
  };
  await w.setProps({ section: sections[form] });
  await flushPromises();
  if (
    form === "invoice" &&
    w.find(".crypto-filter input[name=merchant_id]").exists() &&
    !(
      w.get(".crypto-filter input[name=merchant_id]")
        .element as HTMLInputElement
    ).value
  ) {
    await w.get(".crypto-filter input[name=merchant_id]").setValue("1");
  }
  if (w.find("[data-form=" + form + "]").exists()) return;
  if (w.find("[data-cancel-form]").exists())
    await w.get("[data-cancel-form]").trigger("click");
  await vi.waitFor(() =>
    expect(w.find("[data-open-form=" + form + "]").exists()).toBe(true),
  );
  await w.get("[data-open-form=" + form + "]").trigger("click");
  await flushPromises();
}
afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("keeps a late financial POST response in its original account scope", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const context = ref({
    area: "merchant",
    id: "1",
    issuer: "",
    csrf: "original",
  });
  const c = client();
  let deliver: ((value: unknown) => void) | undefined;
  let sent: Record<string, unknown> = {};
  c.request.mockImplementation(
    async (path: string, options?: { method?: string; body?: unknown }) => {
      if (options?.method === "POST") {
        sent = options.body as Record<string, unknown>;
        return new Promise((resolve) => {
          deliver = resolve;
        }) as never;
      }
      if (path.startsWith("/crypto/channels")) return channelOptions();
      if (path === "/crypto/config") return config;
      return [];
    },
  );
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true },
    global: {
      provide: { [fundsPrincipal212 as symbol]: computed(() => context.value) },
    },
  });
  await reveal(w, "withdrawal");
  await vi.waitFor(() =>
    expect(
      w
        .get("[data-form=withdrawal] button[type=submit]")
        .attributes("disabled"),
    ).toBeUndefined(),
  );
  await w
    .get("[data-form=withdrawal] input[name=address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  await w.get("[data-form=withdrawal] input[name=amount]").setValue("1");
  await w
    .get("[data-form=withdrawal] input[name=current_password]")
    .setValue("private-password");
  await w.get("[data-form=withdrawal]").trigger("submit");
  await vi.waitFor(() => expect(deliver).toBeTypeOf("function"));
  const original = String(sent.request_id);
  c.csrfToken = "replacement";
  context.value = {
    area: "merchant",
    id: "2",
    issuer: "",
    csrf: "replacement",
  };
  await flushPromises();
  deliver!({
    id: "1".repeat(48),
    merchant_id: "1",
    request_id: original,
    network: "tron",
    address: sent.address,
    amount: "1",
    kind: "settlement",
    state: "requested",
    invoice_id: "",
  });
  await flushPromises();
  await reveal(w, "withdrawal");
  expect(
    (
      w.get("[data-form=withdrawal] input[name=request_id]")
        .element as HTMLInputElement
    ).value,
  ).not.toBe(original);
  expect(
    (
      w.get("[data-form=withdrawal] input[name=address]")
        .element as HTMLInputElement
    ).value,
  ).toBe("");
  expect(w.text()).not.toContain("申请已记录");
  const oldStore = new CryptoIntentStore(
    await fundsScope212({
      area: "merchant",
      id: "1",
      issuer: "",
      csrf: "original",
    }),
  );
  expect((await oldStore.read())?.body.request_id).toBe(original);
  w.unmount();
});

const config = {
  enabled: true,
  networks: [
    { network: "tron", enabled: true, fee_bps: 50 },
    { network: "ethereum", enabled: true, fee_bps: 80 },
  ],
};
function channelOptions(networks = config.networks) {
  return {
    items: networks.map((n) => ({
      id: n.network === "tron" ? "41" : "42",
      name: "Synthetic " + n.network,
      network: n.network,
      fee_bps: n.fee_bps,
      min_amount: "0",
      max_amount: "0",
      day_limit: "0",
    })),
  };
}

it.each(["merchant", "admin"] as const)(
  "initializes every blank network form for an Ethereum-only %s account",
  async (area) => {
    const c = client(area);
    c.request.mockImplementation(async (path) => {
      if (path.startsWith("/crypto/channels"))
        return channelOptions([config.networks[1]!]);
      if (path === "/crypto/config")
        return { enabled: true, networks: [config.networks[1]] };
      return [];
    });
    const w = mount(CryptoFundsPage, {
      props: {
        client: c,
        ownSession: true,
        adminClass: area === "admin" ? "super" : undefined,
      },
    });
    await flushPromises();
    const forms = ["invoice", "recipient", "withdrawal"];
    if (area === "admin") forms.push("address", "sweep-retry");
    for (const form of forms) {
      await reveal(w, form);
      expect(
        (w.get(`[data-form=${form}] select`).element as HTMLSelectElement)
          .value,
      ).toBe(form === "invoice" ? "42" : "ethereum");
    }
    expect(
      c.request.mock.calls.some((call) => call[1]?.method === "POST"),
    ).toBe(false);
    w.unmount();
  },
);

it("does not silently change entered networks when refreshing the enabled network list", async () => {
  const c = client("admin");
  let currentConfig = config;
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels"))
      return channelOptions(currentConfig.networks);
    if (path === "/crypto/config") return currentConfig;
    return [];
  });
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  await reveal(w, "invoice");
  await w
    .get("[data-form=invoice] input[name=out_trade_no]")
    .setValue("original-tron-invoice");
  await w.get("[data-form=invoice] input[name=amount]").setValue("10");
  await reveal(w, "withdrawal");
  await w
    .get("[data-form=withdrawal] input[name=address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  await w.get("[data-form=withdrawal] input[name=amount]").setValue("1");
  await reveal(w, "recipient");
  await w
    .get("[data-form=recipient] input[name=recipient_address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  currentConfig = { enabled: true, networks: [config.networks[1]!] };
  expect(
    w.findAll("button").some((button) => button.text() === "刷新记录"),
  ).toBe(false);
  await w.setProps({ section: "overview" });
  await flushPromises();
  for (const form of ["invoice", "recipient", "withdrawal"]) {
    await reveal(w, form);
    expect(
      (w.get(`[data-form=${form}] select`).element as HTMLSelectElement)
        .selectedIndex,
    ).toBe(-1);
  }
  expect(
    (
      w.get("[data-form=withdrawal] input[name=amount]")
        .element as HTMLInputElement
    ).value,
  ).toBe("1");
  expect(c.request.mock.calls.some((call) => call[1]?.method === "POST")).toBe(
    false,
  );
  w.unmount();
});

it("recovers an uncertain original request after refresh and reads it without creating a replacement", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const principal = { area: "merchant", id: "1", issuer: "", csrf: "original" };
  const store = new CryptoIntentStore(await fundsScope212(principal));
  await store.begin("/crypto/withdrawals", {
    merchant_id: "1",
    request_id: "original-pending-request",
    network: "tron",
    address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    amount: "2.500000",
  });
  const c = client();
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    if (path.startsWith("/crypto/withdrawals/by-request"))
      throw new ApiError("not found", 404, -1);
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true },
    global: {
      provide: { [fundsPrincipal212 as symbol]: computed(() => principal) },
    },
  });
  await vi.waitFor(() =>
    expect(w.text()).toContain("original-pending-request"),
  );
  await reveal(w, "withdrawal");
  expect(
    (
      w.get("[data-form=withdrawal] input[name=request_id]")
        .element as HTMLInputElement
    ).value,
  ).toBe("original-pending-request");
  expect(
    (
      w.get("[data-form=withdrawal] input[name=amount]")
        .element as HTMLInputElement
    ).value,
  ).toBe("2.5");
  expect(
    w.get("[data-form=withdrawal] button[type=submit]").attributes("disabled"),
  ).toBeDefined();
  await vi.waitFor(() =>
    expect(
      c.request.mock.calls.some(
        (call) =>
          call[0].startsWith("/crypto/withdrawals/by-request") &&
          call[0].includes("request_id=original-pending-request"),
      ),
    ).toBe(true),
  );
  expect(c.request.mock.calls.some((call) => call[1]?.method === "POST")).toBe(
    false,
  );
  expect((await store.read())?.body.request_id).toBe(
    "original-pending-request",
  );
  w.unmount();
});
function client(area: "merchant" | "admin" = "merchant") {
  return Object.assign(new ApiClient(area), {
    csrfToken: "original",
    request: vi.fn(
      async (
        path: string,
        options?: { method?: string; body?: unknown; signal?: AbortSignal },
      ): Promise<unknown> => {
        if (path.startsWith("/crypto/channels")) return channelOptions();
        if (path === "/crypto/config") return config;
        if (path.startsWith("/crypto/balances"))
          return [
            { network: "tron", available: "1.123456", reserved: "0.000001" },
          ];
        return [];
      },
    ),
  });
}

const monitoringFixture = {
  as_of: "2026-11-18T05:06:07.123456Z",
  window_start: "2026-11-18T00:00:00Z",
  window_end: "2026-11-19T00:00:00Z",
  networks: [
    {
      network: "tron",
      enabled: false,
      deposit_total: 12,
      deposit_used: 7,
      deposit_available: 5,
    },
    {
      network: "ethereum",
      enabled: false,
      deposit_total: 0,
      deposit_used: 0,
      deposit_available: 0,
    },
  ],
  channels: [
    {
      id: "42",
      name: "TRON 生产前复核",
      network: "tron",
      enabled: true,
      archived: false,
      day_limit: "10",
      day_used: "7.123456",
      day_remaining: "2.876544",
      day_usage_by_state: {
        pending: "3.123456",
        underpaid: "0",
        paid: "0",
        overpaid: "0",
        expired: "4",
        late: "0",
      },
    },
    {
      id: "43",
      name: "Ethereum 无额度限制",
      network: "ethereum",
      enabled: false,
      archived: false,
      day_limit: "0",
      day_used: "0",
      day_remaining: null,
      day_usage_by_state: {
        pending: "0",
        underpaid: "0",
        paid: "0",
        overpaid: "0",
        expired: "0",
        late: "0",
      },
    },
  ],
};

it("shows exact platform capacity on the operations page with the runtime disabled", async () => {
  const c = client("admin");
  c.request.mockImplementation(async (path) => {
    if (path === "/crypto/monitoring") return monitoringFixture;
    return { configured: false, enabled: false, networks: [] };
  });
  const w = mount(CryptoFundsPage, {
    props: {
      client: c,
      ownSession: true,
      adminClass: "super",
      section: "operations",
    },
  });
  await flushPromises();
  expect(w.find("[data-testid=crypto-service-disabled]").exists()).toBe(true);
  const monitor = w.get("[data-testid=crypto-monitoring]");
  expect(monitor.text()).toContain("2026-11-19 00:00:00 UTC");
  expect(monitor.get("[data-network=tron]").text()).toContain("网络已关闭");
  expect(monitor.get("[data-network=tron]").text()).toMatch(/未分配\s*5/);
  expect(monitor.get("[data-network=ethereum]").text()).toContain("尚未登记");
  const channel = monitor.get('[data-channel="42"]');
  expect(channel.text()).toContain("7.123456 USDT");
  expect(channel.text()).toContain("2.876544 USDT");
  expect(channel.text()).toMatch(/待付款\s*3\.123456/);
  expect(channel.text()).toMatch(/已过期\s*4/);
  expect(monitor.get('[data-channel="43"]').text()).toContain("不限");
  expect(w.find("[data-open-form=address]").exists()).toBe(false);
  expect(c.request.mock.calls.map(([path]) => path).sort()).toEqual([
    "/crypto/config",
    "/crypto/monitoring",
  ]);
  w.unmount();
});

it("discards stale capacity on refresh failure and recovers an empty monitoring view", async () => {
  const c = client("admin");
  let reply: unknown = monitoringFixture;
  c.request.mockImplementation(async (path) => {
    if (path === "/crypto/config") return config;
    if (path === "/crypto/monitoring") {
      if (reply instanceof Error) throw reply;
      return reply;
    }
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: {
      client: c,
      ownSession: true,
      adminClass: "super",
      section: "operations",
    },
  });
  await flushPromises();
  const reopen = async () => {
    await w.setProps({ section: "invoices" });
    await flushPromises();
    await w.setProps({ section: "operations" });
  };
  expect(w.find('[data-channel="42"]').exists()).toBe(true);
  reply = new ApiError("unavailable", 503, -1);
  await reopen();
  await flushPromises();
  const monitor = () => w.get("[data-testid=crypto-monitoring]");
  expect(monitor().get("[role=alert]").text()).toContain(
    "监控数据暂时无法读取",
  );
  expect(monitor().find('[data-channel="42"]').exists()).toBe(false);
  expect(w.find("[data-open-form=address]").exists()).toBe(true);
  reply = {
    ...monitoringFixture,
    channels: [{ ...monitoringFixture.channels[0], day_remaining: "12" }],
  };
  await reopen();
  await flushPromises();
  expect(monitor().get("[role=alert]").text()).toContain(
    "监控数据暂时无法读取",
  );
  expect(monitor().find('[data-channel="42"]').exists()).toBe(false);
  reply = { ...monitoringFixture, channels: [] };
  await reopen();
  await flushPromises();
  expect(monitor().text()).toContain("暂无 USDT 通道");
  expect(monitor().find("[role=alert]").exists()).toBe(false);
  expect(c.request.mock.calls.some(([, options]) => options?.method)).toBe(
    false,
  );
  w.unmount();
});

it("discards the previous session's pending monitoring response", async () => {
  const original = client("admin");
  let signal: AbortSignal | undefined;
  let complete!: (value: unknown) => void;
  const pending = new Promise<unknown>((resolve) => (complete = resolve));
  original.request.mockImplementation(async (path, options) => {
    if (path === "/crypto/monitoring") {
      signal = options?.signal;
      return pending;
    }
    if (path === "/crypto/config") return config;
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: {
      client: original,
      ownSession: true,
      adminClass: "super",
      section: "operations",
    },
  });
  await flushPromises();
  expect(signal?.aborted).toBe(false);

  const next = client("admin");
  next.csrfToken = "next-session";
  next.request.mockImplementation(async (path) => {
    if (path === "/crypto/config") return config;
    if (path === "/crypto/monitoring")
      return { ...monitoringFixture, channels: [] };
    return [];
  });
  await w.setProps({ client: next });
  await flushPromises();
  expect(signal?.aborted).toBe(true);
  complete(monitoringFixture);
  await flushPromises();
  const monitor = w.get("[data-testid=crypto-monitoring]");
  expect(monitor.text()).toContain("暂无 USDT 通道");
  expect(monitor.find('[data-channel="42"]').exists()).toBe(false);
  w.unmount();
});

it.each([
  ["merchant", undefined, true, "operations"],
  ["admin", "operator", true, "operations"],
  ["admin", "super", false, "operations"],
  ["admin", "super", true, "overview"],
] as const)(
  "restricts monitoring to the platform operations scope (%s %s %s %s)",
  async (area, adminClass, ownSession, section) => {
    const c = client(area);
    const w = mount(CryptoFundsPage, {
      props: { client: c, ownSession, adminClass, section },
    });
    await flushPromises();
    expect(w.find("[data-testid=crypto-monitoring]").exists()).toBe(false);
    expect(
      c.request.mock.calls.some(([path]) => path === "/crypto/monitoring"),
    ).toBe(false);
    w.unmount();
  },
);

it("shows exact six-decimal USDT balances without platform control actions for a merchant", async () => {
  const c = client();
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  expect(w.text()).toContain("1.123456");
  expect(w.text()).toContain("0.000001");
  expect(w.text()).toContain("原链");
  expect(w.find("[data-action=approve]").exists()).toBe(false);
  expect(w.find("[data-form=address]").exists()).toBe(false);
  expect(w.find("input[name=merchant_id]").exists()).toBe(false);
  expect(w.find("[data-form]").exists()).toBe(false);
  await reveal(w, "recipient");
  expect(w.find("[data-form=recipient]").exists()).toBe(true);
  expect(w.text()).toContain("24 小时");
  await reveal(w, "refund");
  expect(w.text()).toContain("退回未入账款项");
  await reveal(w, "withdrawal");
  expect(
    w
      .findAll("select[name=network] option")
      .map((o) => o.text())
      .join(" "),
  ).toContain("Ethereum");
  w.unmount();
});

it("creates a native USDT invoice without a user-supplied fee or CNY quote", async () => {
  const c = client();
  c.request.mockImplementation(
    async (path: string, options?: { method?: string }) => {
      if (path.startsWith("/crypto/channels")) return channelOptions();
      if (path === "/crypto/config") return config;
      if (options?.method === "POST")
        return {
          id: "invoice",
          checkout_url: "/crypto/checkout/invoice?token=private",
        } as never;
      return [];
    },
  );
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  await reveal(w, "invoice");
  await w
    .get("[data-form=invoice] input[name=out_trade_no]")
    .setValue("merchant-order-1");
  await w.get("[data-form=invoice] input[name=amount]").setValue("10.123456");
  await w.get("[data-form=invoice]").trigger("submit");
  await flushPromises();
  const post = c.request.mock.calls.find(
    (call) =>
      call[0] === "/crypto/invoices" &&
      (call[1] as { method?: string })?.method === "POST",
  ) as unknown as [string, { body: Record<string, unknown> }];
  expect(post[1].body).toEqual({
    channel_id: "41",
    out_trade_no: "merchant-order-1",
    network: "tron",
    amount: "10.123456",
    notify_url: "",
    return_url: "",
  });
  expect(w.get("a[data-checkout]").attributes("href")).toBe(
    "/crypto/checkout/invoice?token=private",
  );
  w.unmount();
});

it("discards delayed balances and disables mutations after the session changes", async () => {
  const c = client();
  const deliveries: ((value: unknown) => void)[] = [];
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    if (path.startsWith("/crypto/balances"))
      return new Promise((resolve) => {
        deliveries.push(resolve);
      }) as never;
    return [];
  });
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  c.csrfToken = "replacement";
  await w.setProps({ ownSession: false });
  deliveries[0]!([
    { network: "tron", available: "999.000000", reserved: "0.000000" },
  ]);
  await flushPromises();
  expect(w.text()).not.toContain("999.000000");
  expect(w.find("[data-form=invoice]").exists()).toBe(false);
  w.unmount();
});

it.each([
  { configured: false, enabled: false, networks: [] },
  { enabled: false, networks: [] },
])(
  "shows an unconfigured service without querying history or exposing money forms: %j",
  async (closed) => {
    const c = client("admin");
    c.request.mockImplementation(async (path) => {
      if (path.startsWith("/crypto/channels")) return channelOptions();
      if (path === "/crypto/config") return closed;
      throw new ApiError(
        "USDT 收款与结算尚未启用，当前无法处理此操作",
        503,
        -1,
      );
    });
    const w = mount(CryptoFundsPage, {
      props: { client: c, ownSession: true },
    });
    await flushPromises();
    expect(w.get("[data-testid=crypto-service-disabled]").text()).toContain(
      "尚未启用",
    );
    expect(w.find("[role=alert].error").exists()).toBe(false);
    expect(w.find("form").exists()).toBe(false);
    expect(w.find("input[type=password]").exists()).toBe(false);
    expect(w.text()).not.toContain("对账");
    expect(c.request.mock.calls.map((call) => call[0])).toEqual([
      "/crypto/config",
    ]);
    await w.setProps({ section: "invoices" });
    await flushPromises();
    expect(c.request.mock.calls.map((call) => call[0])).toEqual([
      "/crypto/config",
      "/crypto/config",
    ]);
    w.unmount();
  },
);

it("waits for service configuration before reading any account history", async () => {
  const c = client();
  let deliver: ((value: unknown) => void) | undefined;
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config")
      return new Promise((resolve) => {
        deliver = resolve;
      }) as never;
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "invoices" },
  });
  await flushPromises();
  expect(c.request.mock.calls.map((call) => call[0])).toEqual([
    "/crypto/config",
  ]);
  expect(w.find("[data-form=invoice]").exists()).toBe(false);
  deliver!(config);
  await flushPromises();
  await reveal(w, "invoice");
  expect(w.find("[data-form=invoice]").exists()).toBe(true);
  w.unmount();
});

it("lets the super admin provision the Ethereum address pool while mainnet is closed", async () => {
  const c = client("admin");
  c.request.mockImplementation(async (path) => {
    if (path === "/crypto/config")
      return {
        configured: true,
        enabled: false,
        networks: [{ ...config.networks[1], enabled: false }],
      };
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: {
      client: c,
      adminClass: "super",
      ownSession: true,
      section: "operations",
    },
  });
  await flushPromises();
  expect(w.find("[data-open-form=sweep-retry]").exists()).toBe(false);
  await w.get("[data-open-form=address]").trigger("click");
  const form = w.get("[data-form=address]");
  expect(form.get("select").element.value).toBe("ethereum");
  await form.get('input[autocomplete="off"]').setValue("synthetic-deposit");
  await form.get('input[type="password"]').setValue("local-only-password");
  await form.trigger("submit");
  await flushPromises();
  expect(
    c.request.mock.calls.filter(([, options]) => options?.method === "POST"),
  ).toEqual([
    [
      "/crypto/addresses",
      {
        method: "POST",
        body: {
          network: "ethereum",
          role: "deposit",
          address: "synthetic-deposit",
          current_password: "local-only-password",
        },
      },
    ],
  ]);
  expect(w.text()).toContain("受管地址已登记");
  await w.setProps({ adminClass: "operator" });
  await flushPromises();
  expect(w.find("[data-open-form=address]").exists()).toBe(false);
  w.unmount();
});

it.each([true, false])(
  "keeps configured but paused history readable without money forms (own session %s)",
  async (ownSession) => {
    const c = client("admin");
    c.request.mockImplementation(async (path) => {
      if (path.startsWith("/crypto/channels")) return channelOptions();
      if (path === "/crypto/config")
        return { configured: true, enabled: false, networks: [] };
      if (path.startsWith("/crypto/balances"))
        return [
          { network: "tron", available: "7.123456", reserved: "0.000001" },
        ];
      if (path.startsWith("/crypto/invoices"))
        return [
          {
            id: "historic-invoice",
            out_trade_no: "historic-order",
            network: "tron",
            amount: "7",
            received: "7",
            fee: "0",
            state: "paid",
            expires_at: "2026-01-01T00:00:00Z",
          },
        ];
      return [];
    });
    const w = mount(CryptoFundsPage, { props: { client: c, ownSession } });
    await flushPromises();
    expect(w.get("[data-testid=crypto-service-readonly]").text()).toContain(
      "已暂停",
    );
    expect(w.text()).toContain("7.123456");
    await w.setProps({ section: "invoices" });
    await flushPromises();
    expect(w.text()).toContain("historic-order");
    expect(w.find("[data-form]").exists()).toBe(false);
    expect(w.find("input[type=password]").exists()).toBe(false);
    expect(
      c.request.mock.calls.filter((call) =>
        /\/crypto\/(balances|invoices|withdrawals|recipients)$/.test(call[0]),
      ),
    ).toHaveLength(8);
    expect(
      c.request.mock.calls.some((call) => call[1]?.method === "POST"),
    ).toBe(false);
    w.unmount();
  },
);

it("hides all money forms for a delegated session even when service is enabled", async () => {
  const c = client();
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: false } });
  await flushPromises();
  expect(w.text()).toContain("仅能查看记录");
  expect(w.text()).toContain("1.123456");
  expect(w.find("[data-form]").exists()).toBe(false);
  expect(w.find("input[type=password]").exists()).toBe(false);
  w.unmount();
});

it.each(["config", "balances"])(
  "locks a failed %s refresh, clears stale checkout links, and restores entered drafts only after a successful read",
  async (failure) => {
    const c = client("admin");
    let failing = false;
    c.request.mockImplementation(async (path, options) => {
      if (failing && path.split("?")[0] === "/crypto/" + failure)
        throw new ApiError("读取失败", 503, -1);
      if (path.startsWith("/crypto/channels")) return channelOptions();
      if (path === "/crypto/config") return { configured: true, ...config };
      if (options?.method === "POST")
        return { checkout_url: "/crypto/checkout/private-original" } as never;
      if (path.startsWith("/crypto/balances"))
        return [{ network: "tron", available: "9.654321", reserved: "0" }];
      return [];
    });
    const w = mount(CryptoFundsPage, {
      props: { client: c, ownSession: true },
    });
    await reveal(w, "withdrawal");
    const originalRequest = (
      w.get("input[name=request_id]").element as HTMLInputElement
    ).value;
    await w.get("[data-form=withdrawal] input[name=amount]").setValue("2.5");
    await w.get("input[name=current_password]").setValue("do-not-retain");
    await reveal(w, "invoice");
    await w.get("input[name=merchant_id]").setValue("1");
    await w.get("input[name=out_trade_no]").setValue("preserved-order");
    await w.get("[data-form=invoice] input[name=amount]").setValue("1");
    await w.get("[data-form=invoice]").trigger("submit");
    await flushPromises();
    expect(w.find("[data-checkout]").exists()).toBe(true);
    failing = true;
    await w.setProps({ section: "overview" });
    await flushPromises();
    expect(w.find("[data-form]").exists()).toBe(false);
    expect(w.find("input[type=password]").exists()).toBe(false);
    expect(w.find("[data-checkout]").exists()).toBe(false);
    expect(w.text()).not.toContain("9.654321");
    expect(w.get("[data-testid=crypto-service-error]").text()).toContain(
      "暂时无法读取",
    );
    failing = false;
    await w.setProps({ section: "invoices" });
    await flushPromises();
    expect(w.find("[data-form]").exists()).toBe(false);
    await reveal(w, "invoice");
    expect(
      (w.get("input[name=out_trade_no]").element as HTMLInputElement).value,
    ).toBe("preserved-order");
    expect(
      w.get("[data-form=invoice] button[type=submit]").attributes("disabled"),
    ).toBeUndefined();
    expect(w.find("[data-checkout]").exists()).toBe(false);
    await reveal(w, "withdrawal");
    expect(
      (w.get("input[name=amount]").element as HTMLInputElement).value,
    ).toBe("2.5");
    expect(
      (w.get("input[name=request_id]").element as HTMLInputElement).value,
    ).toBe(originalRequest);
    expect(
      (w.get("input[name=current_password]").element as HTMLInputElement).value,
    ).toBe("");
    expect(
      c.request.mock.calls.filter((call) => call[1]?.method === "POST"),
    ).toHaveLength(1);
    w.unmount();
  },
);

it("prevents a delayed configuration from reviving the previous area", async () => {
  const c = client("admin");
  let deliver: ((value: unknown) => void) | undefined;
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") {
      if (c.area === "admin")
        return new Promise((resolve) => {
          deliver = resolve;
        }) as never;
      return { configured: false, enabled: false, networks: [] };
    }
    return [];
  });
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  // ApiClient is shared by the shell; the area can change before a response settles.
  c.area = "merchant";
  await w.setProps({ ownSession: false });
  deliver!(config);
  await flushPromises();
  expect(w.find("[data-form]").exists()).toBe(false);
  expect(w.find("[data-testid=crypto-service-disabled]").exists()).toBe(true);
  expect(c.request.mock.calls.map((call) => call[0])).toEqual([
    "/crypto/config",
    "/crypto/config",
  ]);
  w.unmount();
});

it("clears a reconciliation read error on a successful refresh and restores normal writes", async () => {
  const c = client("admin");
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    if (path === "/crypto/reconciliation")
      throw new ApiError("对账暂不可用", 503, -1);
    return [];
  });
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await w.setProps({ section: "reconciliation" });
  await flushPromises();
  await w
    .findAll("button")
    .find((button) => button.text() === "查询账本对账结果")!
    .trigger("click");
  await flushPromises();
  expect(w.find("[data-form]").exists()).toBe(false);
  await w.setProps({ section: "overview" });
  await flushPromises();
  expect(w.find("[role=alert].error").exists()).toBe(false);
  await reveal(w, "invoice");
  expect(
    w.get("[data-form=invoice] button[type=submit]").attributes("disabled"),
  ).toBeUndefined();
  expect(c.request.mock.calls.some((call) => call[1]?.method === "POST")).toBe(
    false,
  );
  w.unmount();
});

it("retains an uncertain financial intent through service shutdown and recovers only by reading its original request", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const principal = { area: "merchant", id: "1", issuer: "", csrf: "original" };
  const store = new CryptoIntentStore(await fundsScope212(principal));
  await store.begin("/crypto/withdrawals", {
    merchant_id: "1",
    request_id: "preserved-unknown-request",
    network: "tron",
    address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    amount: "2.500000",
  });
  const c = client();
  let closed = true;
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config")
      return closed
        ? { configured: false, enabled: false, networks: [] }
        : config;
    if (path.startsWith("/crypto/withdrawals/by-request"))
      throw new ApiError("not found", 404, -1);
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true },
    global: {
      provide: { [fundsPrincipal212 as symbol]: computed(() => principal) },
    },
  });
  await vi.waitFor(() =>
    expect(w.find("[data-testid=crypto-service-disabled]").exists()).toBe(true),
  );
  expect(w.find("[data-form]").exists()).toBe(false);
  expect((await store.read())?.body.request_id).toBe(
    "preserved-unknown-request",
  );
  expect(c.request.mock.calls.map((call) => call[0])).toEqual([
    "/crypto/config",
  ]);
  closed = false;
  await w.setProps({ section: "settlements" });
  await vi.waitFor(() =>
    expect(
      c.request.mock.calls.some((call) =>
        call[0].includes("request_id=preserved-unknown-request"),
      ),
    ).toBe(true),
  );
  await reveal(w, "withdrawal");
  expect(
    (
      w.get("[data-form=withdrawal] input[name=request_id]")
        .element as HTMLInputElement
    ).value,
  ).toBe("preserved-unknown-request");
  expect(
    (
      w.get("[data-form=withdrawal] input[name=amount]")
        .element as HTMLInputElement
    ).value,
  ).toBe("2.5");
  expect(
    w.get("[data-form=withdrawal] button[type=submit]").attributes("disabled"),
  ).toBeDefined();
  expect((await store.read())?.body.request_id).toBe(
    "preserved-unknown-request",
  );
  expect(c.request.mock.calls.some((call) => call[1]?.method === "POST")).toBe(
    false,
  );
  w.unmount();
});

it("defaults to a compact overview with no transaction tables or money forms", async () => {
  const w = mount(CryptoFundsPage, {
    props: { client: client(), ownSession: true },
  });
  await flushPromises();
  expect(w.get(".crypto-funds").attributes("data-section")).toBe("overview");
  expect(w.text()).toContain("1.123456");
  expect(w.text()).toContain("TRON");
  expect(w.find("table").exists()).toBe(false);
  expect(w.find("[data-form]").exists()).toBe(false);
  expect(w.find("input[type=password]").exists()).toBe(false);
  expect(w.find("[data-open-form]").exists()).toBe(false);
  w.unmount();
});

it.each([
  [
    "invoices",
    "invoice",
    "invoice-order",
    "settlement-request",
    "refund-request",
  ],
  [
    "settlements",
    "withdrawal",
    "settlement-request",
    "invoice-order",
    "refund-request",
  ],
  [
    "refunds",
    "refund",
    "refund-request",
    "invoice-order",
    "settlement-request",
  ],
  [
    "recipients",
    "recipient",
    "recipient-address",
    "invoice-order",
    "settlement-request",
  ],
  [
    "operations",
    "address",
    "sweep-request",
    "invoice-order",
    "settlement-request",
  ],
] as const)(
  "isolates the %s page and expands only its requested form",
  async (section, form, included, excluded, other) => {
    const c = client("admin");
    c.request.mockImplementation(async (path) => {
      if (path.startsWith("/crypto/channels")) return channelOptions();
      if (path === "/crypto/config") return config;
      if (path.startsWith("/crypto/invoices"))
        return [
          {
            id: "invoice-id",
            out_trade_no: "invoice-order",
            network: "tron",
            amount: "1",
            received: "1",
            fee: "0",
            state: "paid",
            expires_at: "2026-01-01T00:00:00Z",
          },
        ];
      if (path.startsWith("/crypto/withdrawals"))
        return ["settlement", "refund", "exception_refund", "sweep"].map(
          (kind) => ({
            id: kind,
            merchant_id: "1",
            request_id: `${kind === "exception_refund" ? "exception" : kind}-request`,
            network: "tron",
            address: "history-address",
            amount: "1",
            state: "requested",
            kind,
            tx_id: "",
          }),
        );
      if (path.startsWith("/crypto/recipients"))
        return [
          {
            id: "recipient-id",
            merchant_id: "1",
            network: "tron",
            address: "recipient-address",
            state: "approved",
            usable_at: "2026-01-01T00:00:00Z",
          },
        ];
      return [];
    });
    const w = mount(CryptoFundsPage, {
      props: { client: c, ownSession: true, adminClass: "super", section },
    });
    await flushPromises();
    expect(w.get(".crypto-funds").attributes("data-section")).toBe(section);
    expect(w.text()).toContain(included);
    expect(w.text()).not.toContain(excluded);
    expect(w.text()).not.toContain(other);
    if (section === "refunds") expect(w.text()).toContain("exception-request");
    expect(w.find("[data-form]").exists()).toBe(false);
    await w.get(`[data-open-form=${form}]`).trigger("click");
    expect(
      w.findAll("[data-form]").map((node) => node.attributes("data-form")),
    ).toEqual([form]);
    expect(w.find("[data-form=invoice]").exists()).toBe(section === "invoices");
    await w.get("[data-cancel-form]").trigger("click");
    expect(w.find("[data-form]").exists()).toBe(false);
    expect(
      c.request.mock.calls.some((call) => call[1]?.method === "POST"),
    ).toBe(false);
    w.unmount();
  },
);

it("clears secret and selected actions on section changes while retaining the draft request ID", async () => {
  const c = client("admin");
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "settlements" },
  });
  await flushPromises();
  await w.get("[data-open-form=withdrawal]").trigger("click");
  const requestID = (
    w.get("input[name=request_id]").element as HTMLInputElement
  ).value;
  await w.get("input[name=amount]").setValue("2.5");
  await w.get("input[name=current_password]").setValue("private-password");
  await w.setProps({ section: "refunds" });
  expect(w.find("input[type=password]").exists()).toBe(false);
  expect(w.find("[data-form=withdrawal]").exists()).toBe(false);
  await w.setProps({ section: "settlements" });
  await flushPromises();
  await w.get("[data-open-form=withdrawal]").trigger("click");
  expect(
    (w.get("input[name=current_password]").element as HTMLInputElement).value,
  ).toBe("");
  expect(
    (w.get("input[name=request_id]").element as HTMLInputElement).value,
  ).toBe(requestID);
  expect((w.get("input[name=amount]").element as HTMLInputElement).value).toBe(
    "2.5",
  );
  await w.get("[data-cancel-form]").trigger("click");
  await w.get("[data-open-form=withdrawal]").trigger("click");
  expect((w.get("input[name=amount]").element as HTMLInputElement).value).toBe(
    "",
  );
  expect(
    (w.get("input[name=request_id]").element as HTMLInputElement).value,
  ).toBe(requestID);
  w.unmount();
});

it("keeps an in-flight address application locked across pages and preserves an unknown outcome", async () => {
  const c = client();
  let reject: ((error: unknown) => void) | undefined;
  c.request.mockImplementation(async (path, options) => {
    if (options?.method === "POST")
      return new Promise((_resolve, fail) => {
        reject = fail;
      }) as never;
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "recipients" },
  });
  await flushPromises();
  await w.get("[data-open-form=recipient]").trigger("click");
  await w
    .get("input[name=recipient_address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  await w.get("input[type=password]").setValue("private-password");
  await w.get("[data-form=recipient]").trigger("submit");
  await flushPromises();
  await w.setProps({ section: "invoices" });
  await flushPromises();
  await w.get("[data-open-form=invoice]").trigger("click");
  expect(
    w.get("[data-form=invoice] button[type=submit]").attributes("disabled"),
  ).toBeDefined();
  reject!(new ApiError("response lost", 503, -1));
  await flushPromises();
  expect(w.text()).toContain("结果尚无法确认");
  expect(
    w.get("[data-form=invoice] button[type=submit]").attributes("disabled"),
  ).toBeDefined();
  expect(
    c.request.mock.calls.filter((call) => call[1]?.method === "POST"),
  ).toHaveLength(1);
  w.unmount();
});

it("brings the selected address approval into focus and clears it when leaving the page", async () => {
  const c = client("admin");
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    if (path.startsWith("/crypto/recipients"))
      return [
        {
          id: "recipient-pending",
          merchant_id: "9007199254740993",
          network: "tron",
          address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
          state: "requested",
          usable_at: "2026-01-01T00:00:00Z",
        },
      ];
    return [];
  });
  const w = mount(CryptoFundsPage, {
    attachTo: document.body,
    props: { client: c, ownSession: true, section: "recipients" },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((button) => button.text() === "核对地址并审批")!
    .trigger("click");
  const approval = w.get("[data-confirm-action=recipient-approve]");
  expect(document.activeElement).toBe(
    approval.get("input[type=password]").element,
  );
  expect(w.text()).toContain("9007199254740993");
  await approval.get("input[type=password]").setValue("private-password");
  await w.setProps({ section: "invoices" });
  await flushPromises();
  expect(w.find("[data-confirm-action]").exists()).toBe(false);
  expect(w.find("input[type=password]").exists()).toBe(false);
  w.unmount();
});

it("does not reopen the original invoice or checkout after its response arrives on another page", async () => {
  const c = client();
  let deliver: ((value: unknown) => void) | undefined;
  c.request.mockImplementation(async (path, options) => {
    if (options?.method === "POST")
      return new Promise((resolve) => {
        deliver = resolve;
      }) as never;
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "invoices" },
  });
  await reveal(w, "invoice");
  await w.get("input[name=out_trade_no]").setValue("original-order");
  await w.get("input[name=amount]").setValue("1");
  await w.get("[data-form=invoice]").trigger("submit");
  await flushPromises();
  await reveal(w, "refund");
  deliver!({ checkout_url: "/crypto/checkout/old-private-link" });
  await flushPromises();
  expect(w.get(".crypto-funds").attributes("data-section")).toBe("refunds");
  expect(w.find("[data-form=refund]").exists()).toBe(true);
  expect(w.find("[data-form=invoice]").exists()).toBe(false);
  expect(w.find("[data-checkout]").exists()).toBe(false);
  expect(w.text()).not.toContain("收款单已创建");
  w.unmount();
});

it("sends the original authorized financial request once when its intent finishes saving after a page change", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const principal = { area: "merchant", id: "1", issuer: "", csrf: "original" };
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const begin = CryptoIntentStore.prototype.begin;
  const saving = vi
    .spyOn(CryptoIntentStore.prototype, "begin")
    .mockImplementationOnce(async function (this: CryptoIntentStore, ...args) {
      await gate;
      return begin.apply(this, args);
    });
  const c = client();
  let originalRequest = "";
  const record = () => ({
    id: "a".repeat(48),
    merchant_id: "1",
    request_id: originalRequest,
    network: "tron",
    address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
    amount: "2.5",
    kind: "settlement",
    state: "requested",
    invoice_id: "",
  });
  c.request.mockImplementation(async (path, options) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    if (
      options?.method === "POST" ||
      path.startsWith("/crypto/withdrawals/by-request")
    )
      return record();
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "settlements" },
    global: {
      provide: { [fundsPrincipal212 as symbol]: computed(() => principal) },
    },
  });
  await reveal(w, "withdrawal");
  originalRequest = (
    w.get("input[name=request_id]").element as HTMLInputElement
  ).value;
  await w
    .get("input[name=address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  await w.get("input[name=amount]").setValue("2.5");
  await w.get("input[name=current_password]").setValue("captured-password");
  await w.get("[data-form=withdrawal]").trigger("submit");
  expect(saving).toHaveBeenCalledOnce();
  await w.setProps({ section: "refunds" });
  await flushPromises();
  release();
  await vi.waitFor(() =>
    expect(
      c.request.mock.calls.filter((call) => call[1]?.method === "POST"),
    ).toHaveLength(1),
  );
  const post = c.request.mock.calls.find((call) => call[1]?.method === "POST")!;
  expect(post[0]).toBe("/crypto/withdrawals");
  expect(post[1]?.body).toMatchObject({
    request_id: originalRequest,
    current_password: "captured-password",
    amount: "2.5",
  });
  const store = new CryptoIntentStore(await fundsScope212(principal));
  expect((await store.read())?.body.request_id).toBe(originalRequest);
  expect(w.find("[data-form=withdrawal]").exists()).toBe(false);
  await w
    .findAll("button")
    .find((button) => button.text() === "仅查询原请求")!
    .trigger("click");
  await vi.waitFor(() => expect(w.text()).toContain("已找回原请求"));
  expect(await store.read()).toBeNull();
  await reveal(w, "withdrawal");
  expect(
    (w.get("input[name=request_id]").element as HTMLInputElement).value,
  ).toBe(originalRequest);
  expect((w.get("input[name=amount]").element as HTMLInputElement).value).toBe(
    "2.5",
  );
  expect(
    c.request.mock.calls.filter((call) => call[1]?.method === "POST"),
  ).toHaveLength(1);
  w.unmount();
});

it("shows approved sweep records without offering unsupported cancellation or approval", async () => {
  const c = client("admin");
  c.request.mockImplementation(async (path) => {
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    if (path.startsWith("/crypto/withdrawals"))
      return [
        {
          id: "sweep-id",
          merchant_id: "1",
          request_id: "sweep-request",
          network: "tron",
          address: "managed-address",
          amount: "2.5",
          kind: "sweep",
          state: "approved",
          tx_id: "",
        },
      ];
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "operations" },
  });
  await flushPromises();
  expect(w.text()).toContain("sweep-request");
  expect(w.text()).not.toContain("申请核对取消");
  expect(w.find("[data-action=approve]").exists()).toBe(false);
  expect(w.find("[data-confirm-action]").exists()).toBe(false);
  expect(w.find("[data-open-form=sweep-retry]").exists()).toBe(true);
  w.unmount();
});

it("disables draft cancellation while the original HTTP submission is pending", async () => {
  const c = client();
  let reject!: (error: unknown) => void;
  c.request.mockImplementation(async (path, options) => {
    if (options?.method === "POST")
      return new Promise((_resolve, fail) => {
        reject = fail;
      });
    if (path.startsWith("/crypto/channels")) return channelOptions();
    if (path === "/crypto/config") return config;
    return [];
  });
  const w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "invoices" },
  });
  await reveal(w, "invoice");
  await w.get("input[name=out_trade_no]").setValue("pending-original-order");
  await w.get("input[name=amount]").setValue("1");
  await w.get("[data-form=invoice]").trigger("submit");
  await flushPromises();
  expect(w.get("[data-cancel-form]").attributes("disabled")).toBeDefined();
  await w.get("[data-cancel-form]").trigger("click");
  expect(w.find("[data-form=invoice]").exists()).toBe(true);
  expect(
    (w.get("input[name=out_trade_no]").element as HTMLInputElement).value,
  ).toBe("pending-original-order");
  reject(new ApiError("request refused", 422, -1));
  await flushPromises();
  expect(w.get("[data-cancel-form]").attributes("disabled")).toBeUndefined();
  await w.get("[data-cancel-form]").trigger("click");
  expect(w.find("[data-form=invoice]").exists()).toBe(false);
  expect(
    c.request.mock.calls.filter((call) => call[1]?.method === "POST"),
  ).toHaveLength(1);
  w.unmount();
});
