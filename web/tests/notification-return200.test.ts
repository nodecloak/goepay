// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ref } from "vue";
import OrderDetails from "../src/components/OrderDetails.vue";
import { ApiClient } from "../src/api";

const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
const target =
  "https://merchant-return200.invalid/receive?trade_no=ORDER-200&sign=synthetic200";
const envelope = (data: unknown, status = 200) =>
  new Response(JSON.stringify({ code: status === 200 ? 0 : -1, data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
function setup(
  options: {
    area?: "admin" | "merchant";
    status?: string;
    allowed?: unknown;
    ownSession?: boolean;
    reply?: () => Promise<Response>;
    blockedPopup?: boolean;
  } = {},
) {
  const calls: { path: string; method: string; body: unknown }[] = [];
  const signals: AbortSignal[] = [];
  const sequence: string[] = [];
  const popup = {
    opener: window,
    closed: false,
    close: vi.fn(),
    location: { replace: vi.fn(), assign: vi.fn() },
  };
  const opening = vi.spyOn(window, "open").mockImplementation(() => {
    sequence.push("open");
    return options.blockedPopup ? null : (popup as unknown as Window);
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      const method = init?.method || "GET";
      if (path.endsWith("/return-notification")) {
        sequence.push("request");
        calls.push({ path, method, body: JSON.parse(String(init?.body)) });
        if (init?.signal) signals.push(init.signal);
        expect(new Headers(init?.headers).get("X-CSRF-Token")).toBe(
          "csrf-return200",
        );
        return options.reply ? options.reply() : envelope({ url: target });
      }
      if (method !== "GET")
        throw new Error("unrelated payment or async operation was called");
      return envelope({
        order: {
          trade_no: "ORDER-200",
          status: options.status || "paid",
          amount: "123",
          return_url: "https://merchant-return200.invalid/receive",
        },
        can_resend_notification: true,
        can_return_notification: options.allowed ?? true,
      });
    }),
  );
  // App.vue stores its ApiClient in a ref. Exercise the same reactive identity
  // so a CSRF replacement must synchronously invalidate an in-flight popup.
  const client = ref(new ApiClient(options.area || "merchant")).value;
  client.csrfToken = "csrf-return200";
  const wrapper = mount(OrderDetails, {
    props: {
      client,
      tradeNo: "ORDER-200",
      ownSession: options.ownSession ?? true,
    },
    global: {
      stubs: {
        OrderIncomePanel: true,
        OrderPrintingPanel: true,
        PreauthorizationObservation: true,
        ScanRecoveryPanel: true,
      },
    },
  });
  mounted.push(wrapper);
  return { wrapper, client, calls, signals, popup, opening, sequence };
}
function syncButton(wrapper: VueWrapper) {
  return wrapper
    .findAll("button")
    .find((button) => button.text() === "重新通知(同步)");
}

describe("original synchronous order notification", () => {
  it("keeps the asynchronous action first and generates a return only after the user's click", async () => {
    const f = setup();
    await flushPromises();
    const labels = f.wrapper.findAll("button").map((button) => button.text());
    expect(labels).toContain("重新通知(同步)");
    expect(labels.indexOf("重发商户通知")).toBeLessThan(
      labels.indexOf("重新通知(同步)"),
    );
    expect(f.calls).toHaveLength(0);
    expect(f.opening).not.toHaveBeenCalled();
    await syncButton(f.wrapper)!.trigger("click");
    await flushPromises();
    expect(f.sequence).toEqual(["open", "request"]);
    expect(f.calls).toEqual([
      {
        path: "/console/v1/merchant/orders/ORDER-200/return-notification",
        method: "POST",
        body: {},
      },
    ]);
    expect(f.popup.opener).toBeNull();
    expect(f.popup.location.replace).toHaveBeenCalledWith(target);
    expect(f.popup.close).not.toHaveBeenCalled();
    expect(f.wrapper.text()).not.toContain("sign=synthetic200");
    expect(f.wrapper.text()).not.toContain("商户通知已收到成功响应");
  });

  it("uses the administrator endpoint without granting any funds operation", async () => {
    const f = setup({ area: "admin" });
    await flushPromises();
    expect(syncButton(f.wrapper)).toBeDefined();
    await syncButton(f.wrapper)!.trigger("click");
    await flushPromises();
    expect(f.calls[0]?.path).toBe(
      "/console/v1/admin/orders/ORDER-200/return-notification",
    );
    expect(f.popup.location.replace).toHaveBeenCalledTimes(1);
  });

  it.each([
    "https://merchant-return200.invalid/receive?extension=%2F%2B%3D&trade_no=ORDER-200&sign=public200",
    "https://8.8.8.8/receive?extension=%2F%2B%3D&trade_no=ORDER-200&sign=public200",
    "https://[2606:4700:4700::1111]/receive?extension=%2F%2B%3D&trade_no=ORDER-200&sign=public200",
    "https://例子.example/receive?extension=%2F%2B%3D&trade_no=ORDER-200&sign=public200",
  ])(
    "preserves a public return destination and its exact signed query: %s",
    async (url) => {
      const storage = vi.spyOn(Storage.prototype, "setItem");
      const f = setup({ reply: async () => envelope({ url }) });
      await flushPromises();
      expect(syncButton(f.wrapper)).toBeDefined();
      await syncButton(f.wrapper)!.trigger("click");
      await flushPromises();
      expect(f.popup.location.replace).toHaveBeenCalledExactlyOnceWith(url);
      expect(f.popup.close).not.toHaveBeenCalled();
      expect(f.wrapper.html()).not.toContain("sign=public200");
      expect(storage).not.toHaveBeenCalled();
    },
  );

  it.each([
    { status: "pending", allowed: true, ownSession: true },
    { status: "paid", allowed: false, ownSession: true },
    { status: "paid", allowed: "true", ownSession: true },
    { status: "paid", allowed: true, ownSession: false },
  ])(
    "never derives synchronous authority from contradictory facts: %j",
    async (options) => {
      const f = setup(options);
      await flushPromises();
      expect(syncButton(f.wrapper)).toBeUndefined();
      expect(f.calls).toHaveLength(0);
      expect(f.opening).not.toHaveBeenCalled();
    },
  );

  it("does not generate a signed address when the browser blocks the new window", async () => {
    const f = setup({ blockedPopup: true });
    await flushPromises();
    expect(syncButton(f.wrapper)).toBeDefined();
    await syncButton(f.wrapper)!.trigger("click");
    await flushPromises();
    expect(f.calls).toHaveLength(0);
    expect(f.wrapper.text()).toMatch(/弹出|新窗口/);
  });

  it.each([
    { url: "javascript:alert(200)" },
    { url: "https://user:password@merchant-return200.invalid/receive" },
    { url: "//merchant-return200.invalid/receive" },
    // Both the stored spelling and the browser-normalized private destination
    // must fail closed before the reserved window can navigate.
    ...[
      "http://127.1/private200",
      "http://127.1./private200",
      "http://0177.0.0.1/private200",
      "http://0x7f.0.0.1/private200",
      "http://2130706433/private200",
      "http://0x7f000001/private200",
      "http://127\u30020.0.1/private200",
      "http://127\uff0e0.0.1/private200",
      "http://127\uff610.0.1/private200",
      "http://\uff11\uff12\uff17.0.0.1/private200",
      "http://%31%32%37.0.0.1/private200",
      "http://127%2e0.0.1/private200",
      "http://0300.0250.0.1/private200",
      "http://3232235521/private200",
      "http://0xc0a80001/private200",
      "http://0xa9fea9fe/private200",
      "http://127.0.0.1/private200",
      "https://127.0.0.1/private200",
      "http://10.0.0.1/private200",
      "http://192.168.0.1/private200",
      "http://169.254.169.254/private200",
      "http://[::ffff:127.0.0.1]/private200",
      "http://[0:0:0:0:0:0:0:1]/private200",
      "http://[::1]/private200",
      "http://localhost/private200",
      "http://metadata.example.internal/private200",
      "http://0.0.0.0/private200",
      "http://100.64.0.1/private200",
      "http://192.0.2.1/private200",
      "http://198.18.0.1/private200",
      "http://224.0.0.1/private200",
      "http://[fc00::1]/private200",
      "http://[fe80::1]/private200",
      "http://[64:ff9b::7f00:1]/private200",
      "http://[2002:7f00:1::]/private200",
      "http://[2001:db8::1]/private200",
    ].map((url) => ({ url })),
    { url: "https://merchant-return200.invalid/receive", secret: "unexpected" },
    { url: 200 },
    null,
  ])(
    "closes the reserved window instead of opening malformed server data: %j",
    async (data) => {
      const f = setup({ reply: async () => envelope(data) });
      await flushPromises();
      expect(syncButton(f.wrapper)).toBeDefined();
      await syncButton(f.wrapper)!.trigger("click");
      await flushPromises();
      expect(f.popup.location.replace).not.toHaveBeenCalled();
      expect(f.popup.close).toHaveBeenCalledTimes(1);
      expect(f.wrapper.findAll('[role="alert"]').length).toBeGreaterThan(0);
    },
  );

  it("closes an unused window after a failed generation and does not retry automatically", async () => {
    const f = setup({ reply: async () => envelope(null, 403) });
    await flushPromises();
    expect(syncButton(f.wrapper)).toBeDefined();
    await syncButton(f.wrapper)!.trigger("click");
    await flushPromises();
    expect(f.calls).toHaveLength(1);
    expect(f.popup.location.replace).not.toHaveBeenCalled();
    expect(f.popup.close).toHaveBeenCalledTimes(1);
    expect(f.wrapper.text()).not.toContain("通知已排队");
  });

  it.each(["unmount", "trade", "csrf"])(
    "never navigates a late response after the original context changes: %s",
    async (change) => {
      let finish!: (value: Response) => void;
      const f = setup({
        reply: () => new Promise<Response>((resolve) => (finish = resolve)),
      });
      await flushPromises();
      expect(syncButton(f.wrapper)).toBeDefined();
      await syncButton(f.wrapper)!.trigger("click");
      await flushPromises();
      if (change === "unmount") {
        f.wrapper.unmount();
        mounted.splice(mounted.indexOf(f.wrapper), 1);
      } else if (change === "trade")
        await f.wrapper.setProps({ tradeNo: "OTHER-200" });
      else f.client.csrfToken = "replacement200";
      // Cancellation must happen now, even if the server never replies.
      expect(f.signals[0]?.aborted).toBe(true);
      expect(f.popup.close).toHaveBeenCalledTimes(1);
      expect(f.popup.location.replace).not.toHaveBeenCalled();
      finish(envelope({ url: target }));
      await flushPromises();
      expect(f.popup.location.replace).not.toHaveBeenCalled();
      expect(f.popup.close).toHaveBeenCalled();
      expect(f.calls).toHaveLength(1);
    },
  );
});
