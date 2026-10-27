// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  flushPromises,
  mount,
  type DOMWrapper,
  type VueWrapper,
} from "@vue/test-utils";
import { nextTick, ref } from "vue";
import OrderDetails from "../src/components/OrderDetails.vue";
import { ApiClient } from "../src/api";

/*
 * Original evidence: epay_pro/public/static/assets/index-lAIAtQd1.js renders
 * “编辑接口订单号”, an “接口订单号” input, and “取消” / “确定”. Its API wrapper
 * order-Cn9IuGiH.js POSTs { trade_no, api_trade_no } to order/editApiTradeNo.
 *
 * Deliberate safety difference, agreed with root: this UI edits a display alias
 * in a separate resource. It must never overwrite reporting.api_trade_no, the
 * verified provider receipt, or any payment/refund/notification source.
 * GET /orders/:trade/api-trade-alias returns the six fields in aliasView().
 * PATCH accepts ONLY { api_trade_no_alias, expected_fingerprint }.
 * The existing detail response supplies can_edit_api_trade_alias: true; the
 * alias resource additionally supplies strict can_update authority and a CAS.
 * Clearing the input removes only the alias. Unknown outcomes require a read.
 *
 * These tests mount the existing OrderDetails and real ApiClient. They import
 * no future production symbol. Only the HTTP transport and unrelated children
 * are replaced; every unexpected request is a fixture failure.
 */
const TRADE = "ORDER-ALIAS-SYNTHETIC";
const MERCHANT = "9007199254740993";
const VERIFIED = "VERIFIED-PROVIDER-RECEIPT-ALIAS";
const FINGERPRINT = "a".repeat(64);
const NEXT_FINGERPRINT = "b".repeat(64);
const ALIAS_PATH = `/console/v1/admin/orders/${TRADE}/api-trade-alias`;
const mounted: VueWrapper[] = [];
const unexpected: string[] = [];

afterEach(() => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount());
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  expect(unexpected.splice(0)).toEqual([]);
});

function aliasView(alias = "", trade = TRADE, merchant = MERCHANT) {
  return {
    trade_no: trade,
    merchant_id: merchant,
    api_trade_no_alias: alias,
    verified_api_trade_no: VERIFIED,
    fingerprint: FINGERPRINT,
    can_update: true,
  };
}
function response(data: unknown, status = 200, message = "") {
  return new Response(
    JSON.stringify({ code: status === 200 ? 0 : -1, message, data }),
    { status, headers: { "Content-Type": "application/json" } },
  );
}
type Call = {
  path: string;
  method: string;
  body: unknown;
  csrf: string | null;
  signal: AbortSignal | null;
};
function client(
  csrf = "csrf-api-trade-alias",
  area: "admin" | "merchant" = "admin",
) {
  const value = ref(new ApiClient(area)).value;
  value.csrfToken = csrf;
  return value;
}
function setup(
  options: {
    area?: "admin" | "merchant";
    ownSession?: boolean | "omitted";
    allowed?: unknown;
    csrf?: string;
    initialAlias?: string;
    readReply?: (number: number, call: Call) => Promise<Response>;
    writeReply?: (call: Call) => Promise<Response>;
  } = {},
) {
  const calls: Call[] = [];
  let readNumber = 0;
  let state = aliasView(options.initialAlias);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const call: Call = {
        path: String(input),
        method: init?.method || "GET",
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
        csrf: new Headers(init?.headers).get("X-CSRF-Token"),
        signal: init?.signal || null,
      };
      calls.push(call);
      const match = call.path.match(
        /^\/console\/v1\/(admin|merchant)\/orders\/([^/]+)(\/api-trade-alias)?$/,
      );
      if (match) {
        const trade = decodeURIComponent(match[2]);
        if (!match[3] && call.method === "GET")
          return response({
            order: {
              trade_no: trade,
              merchant_id: MERCHANT,
              amount: "123",
              refunded_amount: "0",
              status: "paid",
            },
            reporting: { api_trade_no: VERIFIED },
            can_resend_notification: false,
            can_return_notification: false,
            can_view_suborders: false,
            can_edit_api_trade_alias: Object.prototype.hasOwnProperty.call(
              options,
              "allowed",
            )
              ? options.allowed
              : true,
          });
        if (match[3] && call.method === "GET") {
          readNumber++;
          return options.readReply
            ? options.readReply(readNumber, call)
            : response({ ...state, trade_no: trade });
        }
        if (match[3] && call.method === "PATCH") {
          if (options.writeReply) return options.writeReply(call);
          const body = call.body as Record<string, unknown>;
          state = {
            ...state,
            api_trade_no_alias: String(body.api_trade_no_alias),
            fingerprint: NEXT_FINGERPRINT,
          };
          return response(state);
        }
      }
      unexpected.push(`${call.method} ${call.path}`);
      throw new Error(
        `Unexpected order-alias request: ${call.method} ${call.path}`,
      );
    }),
  );
  const api = client(options.csrf ?? "csrf-api-trade-alias", options.area);
  const wrapper = mount(OrderDetails, {
    props: {
      client: api,
      tradeNo: TRADE,
      ...(options.ownSession === "omitted"
        ? {}
        : { ownSession: options.ownSession ?? true }),
    },
    attachTo: document.body,
    global: {
      stubs: {
        Teleport: true,
        OrderIncomePanel: true,
        OrderPrintingPanel: true,
        NotificationDeliveryPanel: true,
        NotificationReturnAction200: true,
        PreauthorizationObservation: true,
        ScanRecoveryPanel: true,
      },
    },
  });
  mounted.push(wrapper);
  return {
    wrapper,
    api,
    calls,
    reads: () =>
      calls.filter(
        (call) =>
          call.path.endsWith("/api-trade-alias") && call.method === "GET",
      ),
    writes: () => calls.filter((call) => call.method !== "GET"),
  };
}
function editButton(wrapper: VueWrapper) {
  return wrapper
    .findAll("button")
    .find((button) =>
      [
        button.text(),
        button.attributes("aria-label"),
        button.attributes("title"),
      ].includes("编辑接口订单号"),
    );
}
function dialog(wrapper: VueWrapper) {
  return wrapper.find('[role="dialog"][aria-label="编辑接口订单号"]');
}
function button(wrapper: VueWrapper | DOMWrapper<Element>, label: string) {
  const found = wrapper.findAll("button").find((item) => item.text() === label);
  expect(found, `the actual dialog must provide “${label}”`).toBeDefined();
  return found!;
}
async function open(wrapper: VueWrapper) {
  await flushPromises();
  const entry = editButton(wrapper);
  expect(
    entry,
    "OrderDetails must expose the original edit action for an authorized administrator",
  ).toBeDefined();
  await entry!.trigger("click");
  await flushPromises();
  expect(
    dialog(wrapper).exists(),
    "the edit action must open its real dialog",
  ).toBe(true);
  return dialog(wrapper);
}
async function enter(wrapper: VueWrapper, value: string) {
  const modal = await open(wrapper);
  await modal.get('input[placeholder="请输入接口订单号"]').setValue(value);
  return modal;
}
function deferredResponse() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("administrator original API trade number editor with an independent display alias", () => {
  it("opens the original dialog without pre-filling the verified receipt into the alias", async () => {
    const f = setup();
    await flushPromises();
    expect(f.wrapper.text()).toContain(VERIFIED);
    expect(f.reads()).toHaveLength(0);
    const modal = await open(f.wrapper);
    expect(modal.text()).toContain("接口订单号");
    expect(modal.text()).toContain("显示别名");
    expect(modal.text()).toContain(VERIFIED);
    expect((modal.get("input").element as HTMLInputElement).value).toBe("");
    expect(f.reads()).toHaveLength(1);
    expect(f.reads()[0].path).toBe(ALIAS_PATH);
    expect(f.writes()).toHaveLength(0);
  });

  it("prefills only the stored alias and escapes it as text", async () => {
    const value = '<img src=x onerror="window.aliasInjection=true">';
    const f = setup({ initialAlias: value });
    const modal = await open(f.wrapper);
    expect((modal.get("input").element as HTMLInputElement).value).toBe(value);
    expect(f.wrapper.find("img").exists()).toBe(false);
    expect(f.wrapper.text()).toContain(VERIFIED);
    expect(f.writes()).toHaveLength(0);
  });

  it.each(["取消", "Escape"])(
    "%s discards the draft, restores focus, and leaves the order detail open",
    async (action) => {
      const f = setup();
      const modal = await enter(f.wrapper, "CANCELLED-DISPLAY-ALIAS");
      if (action === "Escape")
        await modal.trigger("keydown", { key: "Escape" });
      else await button(modal, "取消").trigger("click");
      await flushPromises();
      expect(dialog(f.wrapper).exists()).toBe(false);
      expect(document.activeElement).toBe(editButton(f.wrapper)?.element);
      expect(f.wrapper.emitted("close")).toBeUndefined();
      expect(f.writes()).toHaveLength(0);
      expect(f.wrapper.text()).toContain(VERIFIED);
      const reopened = await open(f.wrapper);
      expect((reopened.get("input").element as HTMLInputElement).value).toBe(
        "",
      );
    },
  );

  it("confirms an exact alias-only PATCH with CSRF and keeps the verified receipt separately visible", async () => {
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const f = setup();
    const modal = await enter(f.wrapper, "DISPLAY-ALIAS-A");
    expect(f.writes()).toHaveLength(0);
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    expect(f.writes()[0]).toMatchObject({
      path: ALIAS_PATH,
      method: "PATCH",
      csrf: "csrf-api-trade-alias",
      body: {
        api_trade_no_alias: "DISPLAY-ALIAS-A",
        expected_fingerprint: FINGERPRINT,
      },
    });
    expect(Object.keys(f.writes()[0].body as object).sort()).toEqual([
      "api_trade_no_alias",
      "expected_fingerprint",
    ]);
    expect(f.wrapper.text()).toContain("DISPLAY-ALIAS-A");
    expect(f.wrapper.text()).toContain(VERIFIED);
    expect(f.wrapper.text()).toContain("显示别名");
    expect(storage).not.toHaveBeenCalled();
  });

  it("clears only the alias and retains the original verified receipt", async () => {
    const f = setup({ initialAlias: "OLD-ALIAS-TO-CLEAR" });
    const modal = await enter(f.wrapper, "");
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    expect(f.writes()[0].body).toEqual({
      api_trade_no_alias: "",
      expected_fingerprint: FINGERPRINT,
    });
    expect(f.wrapper.text()).not.toContain("OLD-ALIAS-TO-CLEAR");
    expect(f.wrapper.text()).toContain(VERIFIED);
  });

  it("does not issue a second PATCH while a confirmation is pending", async () => {
    const pending = deferredResponse();
    const f = setup({ writeReply: () => pending.promise });
    const modal = await enter(f.wrapper, "PENDING-ALIAS");
    const confirm = button(modal, "确定");
    await confirm.trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    expect(confirm.attributes("disabled")).toBeDefined();
    await confirm.trigger("click");
    expect(f.writes()).toHaveLength(1);
    expect(f.wrapper.text()).not.toMatch(/修改成功|保存成功/);
    pending.resolve(
      response({
        ...aliasView("PENDING-ALIAS"),
        fingerprint: NEXT_FINGERPRINT,
      }),
    );
    await flushPromises();
    expect(f.wrapper.text()).toContain(VERIFIED);
  });

  it.each([
    { name: "merchant area", area: "merchant" as const },
    { name: "delegated session", ownSession: false },
    { name: "missing own-session evidence", ownSession: "omitted" as const },
    { name: "false capability", allowed: false },
    { name: "string capability", allowed: "true" },
    { name: "missing capability", allowed: undefined },
    { name: "missing CSRF", csrf: "" },
  ])("does not expose an editor for $name", async (options) => {
    const f = setup(options);
    await flushPromises();
    expect(editButton(f.wrapper)).toBeUndefined();
    expect(f.reads()).toHaveLength(0);
    expect(f.writes()).toHaveLength(0);
    expect(f.wrapper.text()).toContain(VERIFIED);
  });

  it("keeps an alias resource with revoked update authority read-only", async () => {
    const f = setup({
      readReply: async () => response({ ...aliasView(), can_update: false }),
    });
    const modal = await open(f.wrapper);
    expect(button(modal, "确定").attributes("disabled")).toBeDefined();
    expect(f.writes()).toHaveLength(0);
  });

  it.each([
    { name: "null response", value: null },
    { name: "wrong trade", value: { ...aliasView(), trade_no: "OTHER-TRADE" } },
    { name: "wrong merchant", value: { ...aliasView(), merchant_id: "2" } },
    {
      name: "mismatched verified receipt",
      value: { ...aliasView(), verified_api_trade_no: "FORGED-RECEIPT" },
    },
    {
      name: "non-string alias",
      value: { ...aliasView(), api_trade_no_alias: {} },
    },
    {
      name: "malformed fingerprint",
      value: { ...aliasView(), fingerprint: "not-a-fingerprint" },
    },
    {
      name: "string update permission",
      value: { ...aliasView(), can_update: "true" },
    },
  ])("fails closed on an unverifiable alias read: $name", async ({ value }) => {
    const f = setup({ readReply: async () => response(value) });
    const modal = await open(f.wrapper);
    expect(modal.find('[role="alert"]').exists()).toBe(true);
    expect(button(modal, "确定").attributes("disabled")).toBeDefined();
    expect(f.writes()).toHaveLength(0);
    expect(f.wrapper.text()).toContain(VERIFIED);
    expect(f.wrapper.text()).not.toContain("FORGED-RECEIPT");
  });

  it("does not allow saving when the independent alias read fails", async () => {
    const f = setup({
      readReply: async () => response(null, 503, "别名读取不可用"),
    });
    const modal = await open(f.wrapper);
    expect(modal.text()).toContain("别名读取不可用");
    expect(button(modal, "确定").attributes("disabled")).toBeDefined();
    expect(f.writes()).toHaveLength(0);
  });

  it("requires a new read and a new confirmation after CAS409", async () => {
    let writes = 0;
    const f = setup({
      readReply: async (number) =>
        response(
          number === 1
            ? aliasView()
            : {
                ...aliasView("CONCURRENT-ALIAS"),
                fingerprint: NEXT_FINGERPRINT,
              },
        ),
      writeReply: async () =>
        ++writes === 1
          ? response(null, 409, "别名已由其他管理员更新，请重新读取")
          : response({
              ...aliasView("REVIEWED-ALIAS"),
              fingerprint: "c".repeat(64),
            }),
    });
    let modal = await enter(f.wrapper, "STALE-DRAFT");
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    expect(
      button(dialog(f.wrapper), "确定").attributes("disabled"),
    ).toBeDefined();
    expect(f.wrapper.text()).not.toMatch(/修改成功|保存成功/);
    await button(dialog(f.wrapper), "重新读取").trigger("click");
    await flushPromises();
    modal = dialog(f.wrapper);
    expect((modal.get("input").element as HTMLInputElement).value).toBe(
      "CONCURRENT-ALIAS",
    );
    expect(f.writes()).toHaveLength(1);
    await modal.get("input").setValue("REVIEWED-ALIAS");
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(2);
    expect(f.writes()[1].body).toEqual({
      api_trade_no_alias: "REVIEWED-ALIAS",
      expected_fingerprint: NEXT_FINGERPRINT,
    });
    expect(f.wrapper.text()).toContain(VERIFIED);
  });

  it("never retries an unknown PATCH and reconciles a committed alias through a read", async () => {
    const f = setup({
      readReply: async (number) =>
        response(
          number === 1
            ? aliasView()
            : {
                ...aliasView("COMMITTED-BUT-RESPONSE-LOST"),
                fingerprint: NEXT_FINGERPRINT,
              },
        ),
      writeReply: async () => {
        throw new TypeError("synthetic response lost");
      },
    });
    const modal = await enter(f.wrapper, "COMMITTED-BUT-RESPONSE-LOST");
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    expect(
      button(dialog(f.wrapper), "确定").attributes("disabled"),
    ).toBeDefined();
    expect(f.wrapper.text()).not.toMatch(/修改成功|保存成功/);
    await button(dialog(f.wrapper), "确定").trigger("click");
    expect(f.writes()).toHaveLength(1);
    await button(dialog(f.wrapper), "重新读取").trigger("click");
    await flushPromises();
    expect(
      (dialog(f.wrapper).get("input").element as HTMLInputElement).value,
    ).toBe("COMMITTED-BUT-RESPONSE-LOST");
    expect(f.writes()).toHaveLength(1);
    expect(f.wrapper.text()).toContain(VERIFIED);
  });

  it.each([403, 422])(
    "keeps the old alias after an explicit HTTP%s rejection",
    async (status) => {
      const f = setup({
        initialAlias: "PREVIOUS-ALIAS",
        writeReply: async () => response(null, status, "当前不能修改别名"),
      });
      const modal = await enter(f.wrapper, "REJECTED-ALIAS");
      await button(modal, "确定").trigger("click");
      await flushPromises();
      expect(f.writes()).toHaveLength(1);
      expect(f.wrapper.text()).toContain("当前不能修改别名");
      expect(f.wrapper.text()).not.toMatch(/修改成功|保存成功/);
      expect(f.wrapper.text()).toContain(VERIFIED);
      await button(dialog(f.wrapper), "取消").trigger("click");
      await flushPromises();
      expect(f.wrapper.text()).not.toContain("REJECTED-ALIAS");
    },
  );

  it.each(["client", "csrf", "trade", "own-session", "unmount"])(
    "discards a late alias read after %s changes",
    async (change) => {
      const pending = deferredResponse();
      const f = setup({ readReply: () => pending.promise });
      await open(f.wrapper);
      expect(f.reads()).toHaveLength(1);
      const read = f.reads()[0];
      if (change === "client")
        await f.wrapper.setProps({ client: client("new-alias-context") });
      else if (change === "csrf") f.api.csrfToken = "new-alias-context";
      else if (change === "trade")
        await f.wrapper.setProps({ tradeNo: "NEW-ORDER-ALIAS" });
      else if (change === "own-session")
        await f.wrapper.setProps({ ownSession: false });
      else f.wrapper.unmount();
      await nextTick();
      expect(read.signal?.aborted).toBe(true);
      pending.resolve(response(aliasView("LATE-ALIAS-READ")));
      await flushPromises();
      expect(f.wrapper.text()).not.toContain("LATE-ALIAS-READ");
      expect(f.writes()).toHaveLength(0);
      expect(f.wrapper.emitted("updated")).toBeUndefined();
    },
  );
});

it("does not restore an old edit capability when the own-session flag changes away and back", async () => {
  const f = setup();
  await open(f.wrapper);
  await f.wrapper.setProps({ ownSession: false });
  expect(dialog(f.wrapper).exists()).toBe(false);
  await f.wrapper.setProps({ ownSession: true });
  expect(editButton(f.wrapper)).toBeUndefined();
  expect(f.writes()).toHaveLength(0);
});

it.each(["client", "csrf", "trade", "own-session", "unmount"])(
  "does not publish a late alias PATCH after %s changes",
  async (change) => {
    const pending = deferredResponse();
    const f = setup({ writeReply: () => pending.promise });
    const modal = await enter(f.wrapper, "LATE-SAVED-ALIAS");
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    const write = f.writes()[0];
    if (change === "client")
      await f.wrapper.setProps({ client: client("new-alias-write-context") });
    else if (change === "csrf") f.api.csrfToken = "new-alias-write-context";
    else if (change === "trade")
      await f.wrapper.setProps({ tradeNo: "NEW-ORDER-ALIAS" });
    else if (change === "own-session")
      await f.wrapper.setProps({ ownSession: false });
    else f.wrapper.unmount();
    await nextTick();
    expect(write.signal?.aborted).toBe(true);
    pending.resolve(
      response({
        ...aliasView("LATE-SAVED-ALIAS"),
        fingerprint: NEXT_FINGERPRINT,
      }),
    );
    await flushPromises();
    expect(f.wrapper.text()).not.toContain("LATE-SAVED-ALIAS");
    expect(f.wrapper.text()).not.toContain("显示别名已保存");
    expect(f.wrapper.emitted("updated")).toBeUndefined();
    expect(f.writes()).toHaveLength(1);
  },
);

it.each([
  { name: "different alias", value: aliasView("UNCONFIRMED-OTHER-ALIAS") },
  {
    name: "different verified receipt",
    value: {
      ...aliasView("REQUESTED-ALIAS"),
      verified_api_trade_no: "FORGED-RECEIPT",
    },
  },
  {
    name: "missing fingerprint",
    value: { ...aliasView("REQUESTED-ALIAS"), fingerprint: undefined },
  },
])(
  "requires a read after an unverifiable successful PATCH: $name",
  async ({ value }) => {
    const f = setup({
      initialAlias: "OLD-CONFIRMED-ALIAS",
      writeReply: async () => response(value),
    });
    const modal = await enter(f.wrapper, "REQUESTED-ALIAS");
    await button(modal, "确定").trigger("click");
    await flushPromises();
    expect(
      button(dialog(f.wrapper), "确定").attributes("disabled"),
    ).toBeDefined();
    expect(f.wrapper.text()).not.toContain("显示别名已保存");
    expect(f.wrapper.text()).not.toContain("FORGED-RECEIPT");
    expect(f.wrapper.text()).toContain(VERIFIED);
    await button(dialog(f.wrapper), "取消").trigger("click");
    await flushPromises();
    expect(f.wrapper.text()).toContain("OLD-CONFIRMED-ALIAS");
    expect(f.wrapper.text()).not.toContain("REQUESTED-ALIAS");
    expect(f.writes()).toHaveLength(1);
  },
);
