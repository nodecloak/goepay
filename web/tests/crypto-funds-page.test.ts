import { afterEach, expect, it, vi } from "vitest";
import { computed, ref } from "vue";
import { webcrypto } from "node:crypto";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import { fundsPrincipal212, fundsScope212 } from "../src/funds-intent212";
import { CryptoIntentStore } from "../src/crypto-intent";
import CryptoFundsPage from "../src/components/CryptoFundsPage.vue";
afterEach(() => {
  localStorage.clear();
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

it.each(["merchant", "admin"] as const)(
  "initializes every blank network form for an Ethereum-only %s account",
  async (area) => {
    const c = client(area);
    c.request.mockImplementation(async (path) => {
      if (path === "/crypto/config")
        return { enabled: true, networks: [config.networks[1]] };
      return [];
    });
    const w = mount(CryptoFundsPage, {
      props: { client: c, ownSession: true },
    });
    await flushPromises();
    const forms = ["invoice", "recipient", "withdrawal"];
    if (area === "admin") forms.push("address", "sweep-retry");
    for (const form of forms)
      expect(
        (w.get(`[data-form=${form}] select`).element as HTMLSelectElement)
          .value,
      ).toBe("ethereum");
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
    if (path === "/crypto/config") return currentConfig;
    return [];
  });
  const w = mount(CryptoFundsPage, { props: { client: c, ownSession: true } });
  await flushPromises();
  await w
    .get("[data-form=invoice] input[name=out_trade_no]")
    .setValue("original-tron-invoice");
  await w.get("[data-form=invoice] input[name=amount]").setValue("10");
  await w
    .get("[data-form=withdrawal] input[name=address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  await w.get("[data-form=withdrawal] input[name=amount]").setValue("1");
  await w
    .get("[data-form=recipient] input[name=recipient_address]")
    .setValue("TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE");
  currentConfig = { enabled: true, networks: [config.networks[1]!] };
  await w
    .findAll("button")
    .find((button) => button.text() === "刷新记录")!
    .trigger("click");
  await flushPromises();
  for (const form of ["invoice", "recipient", "withdrawal"])
    expect(
      (w.get(`[data-form=${form}] select`).element as HTMLSelectElement)
        .selectedIndex,
    ).toBe(-1);
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
    request: vi.fn(async (path: string, options?: { method?: string }) => {
      if (path === "/crypto/config") return config;
      if (path.startsWith("/crypto/balances"))
        return [
          { network: "tron", available: "1.123456", reserved: "0.000001" },
        ];
      return [];
    }),
  });
}

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
  expect(w.find("[data-form=recipient]").exists()).toBe(true);
  expect(w.text()).toContain("24 小时");
  expect(w.text()).toContain("退回未入账款项");
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
  expect(
    w.get("[data-form=invoice] button[type=submit]").attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});
