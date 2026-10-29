// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  flushPromises,
  mount,
  type DOMWrapper,
  type VueWrapper,
} from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import AccountPage from "../src/components/AccountPage.vue";

const endpoint = "/console/v1/merchant/account/event-subscription";
const merchantId = "9007199254740993";
const callback = "https://merchant-refund201.invalid/events";
const initial = {
  event_url: callback,
  event_types: ["refund"],
  fingerprint: "a".repeat(64),
  consecutive_failures: 0,
  last_delivery_state: "none",
  subscription_notice: "",
};
type Call = {
  path: string;
  method: string;
  body: unknown;
  headers: Headers;
  signal?: AbortSignal | null;
};
type Reply = (call: Call) => Promise<Response>;
const mounted: VueWrapper[] = [];

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function response(data: unknown, status = 200, message = "") {
  return new Response(
    JSON.stringify({ code: status === 200 ? 0 : status, data, message }),
    { status },
  );
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
function newClient() {
  const client = reactive(new ApiClient("merchant"));
  client.csrfToken = "refund201-own-session";
  return client;
}
async function setup(
  options: {
    state?: unknown;
    reply?: Reply;
    ownSession?: boolean;
    reactiveClient?: boolean;
  } = {},
) {
  const calls: Call[] = [];
  const client =
    options.reactiveClient === false ? new ApiClient("merchant") : newClient();
  client.csrfToken = "refund201-own-session";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const path = String(input);
      const call: Call = {
        path,
        method: init?.method || "GET",
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
        headers: new Headers(init?.headers),
        signal: init?.signal,
      };
      calls.push(call);
      if (path === endpoint)
        return options.reply
          ? options.reply(call)
          : response(options.state ?? initial);
      if (path === "/console/v1/merchant/account/api-info")
        return response({
          pid: merchantId,
          algorithm: "MD5",
          api_key_masked: "synthetic****201",
        });
      if (path === "/console/v1/merchant/account/profile")
        return response({
          uid: merchantId,
          name: "Synthetic201",
          email: "",
          has_pwd: true,
        });
      throw new Error(`Unexpected request: ${call.method} ${path}`);
    }),
  );
  const wrapper = mount(AccountPage, {
    attachTo: document.body,
    props: { client, section: "api-info" },
    // These become declared props when the parent adds the real mounting hook.
    // Keeping them as attrs makes the pre-feature RED compile against AccountPage199.
    attrs: { ownSession: options.ownSession ?? true, merchantId },
    global: {
      stubs: {
        MerchantRSAKeyRotation: {
          template: "<section><h2>RSA 签名方式</h2></section>",
        },
        MerchantSignaturePolicy: {
          template: "<section><h2>签名方式设置</h2></section>",
        },
        MerchantEmailBinding197: true,
      },
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return {
    wrapper,
    client,
    calls,
    events: () => calls.filter((call) => call.path === endpoint),
  };
}
function panel(wrapper: VueWrapper) {
  const found = wrapper
    .findAll("section")
    .find((section) => section.attributes("aria-label") === "事件消息订阅设置");
  expect(
    found,
    "API information must mount the event subscription settings after signing settings",
  ).toBeDefined();
  return found!;
}
function button(
  scope: { findAll(selector: string): DOMWrapper<Element>[] },
  name: string,
) {
  const found = scope.findAll("button").find((entry) => entry.text() === name);
  expect(found, `visible button ${name}`).toBeDefined();
  return found!;
}
async function prepare(wrapper: VueWrapper, url = callback, enabled = true) {
  const area = panel(wrapper);
  await area.get('[name="event_url"]').setValue(url);
  await area.get('[name="event_types"][value="refund"]').setValue(enabled);
  await area.get("form").trigger("submit");
  await flushPromises();
  return area;
}
async function confirm(wrapper: VueWrapper, label = "确认保存") {
  await button(panel(wrapper), label).trigger("click");
  await flushPromises();
}

describe("original API information refund event subscription", () => {
  it("mounts after signing settings with the original URL and refund fields, without a complaint option", async () => {
    const f = await setup();
    const area = panel(f.wrapper);
    const headings = f.wrapper.findAll("h2").map((entry) => entry.text());
    expect(headings.indexOf("事件消息订阅设置")).toBeGreaterThan(
      headings.indexOf("签名方式设置"),
    );
    expect(area.text()).toContain("订阅URL");
    expect(area.text()).toContain("订阅事件");
    expect(area.text()).toContain("订单退款");
    expect(area.text()).toContain("交易投诉");
    expect(area.text()).toContain("RSA");
    expect(
      area
        .findAll('input[name="event_types"]')
        .map((entry) => entry.attributes("value")),
    ).toEqual(["refund", "complain"]);
    // The complaint option is a real, enabled checkbox now.
    const complain = area
      .findAll('input[name="event_types"]')
      .find((e) => e.attributes("value") === "complain");
    expect(complain!.attributes("disabled")).toBeUndefined();
    expect(area.get('[name="event_url"]').attributes("maxlength")).toBe("500");
    expect(area.get('[name="event_url"]').element).toHaveProperty(
      "value",
      callback,
    );
    expect(area.find('input[value="refund"]').element).toHaveProperty(
      "checked",
      true,
    );
    expect(f.events().map((call) => call.method)).toEqual(["GET"]);
  });

  it("subscribes to 交易投诉 and submits complain in event_types", async () => {
    const saved = {
      ...initial,
      event_types: ["refund", "complain"],
      fingerprint: "b".repeat(64),
    };
    const f = await setup({
      reply: async (call) =>
        response(call.method === "PATCH" ? saved : initial),
    });
    const area = panel(f.wrapper);
    // refund already checked from initial; also check complain, then save.
    await area.get('input[value="complain"]').setValue(true);
    await area.get("form").trigger("submit");
    await flushPromises();
    await button(panel(f.wrapper), "确认保存").trigger("click");
    await flushPromises();
    const write = f.events().find((c) => c.method === "PATCH");
    expect(write).toBeDefined();
    expect(write!.body).toMatchObject({
      event_url: callback,
      event_types: ["refund", "complain"],
    });
    expect(panel(f.wrapper).text()).toContain("交易投诉");
  });

  it("keeps the empty default disabled until a valid URL and refund selection are explicitly confirmed", async () => {
    const saved = {
      ...initial,
      event_url: "https://merchant-refund201.invalid/new",
      fingerprint: "b".repeat(64),
    };
    const f = await setup({
      reply: async (call) =>
        response(
          call.method === "PATCH"
            ? saved
            : { ...initial, event_url: "", event_types: [] },
        ),
    });
    const area = panel(f.wrapper);
    expect(area.get('[name="event_types"]').element).toHaveProperty(
      "checked",
      false,
    );
    await area.get('[name="event_types"]').setValue(true);
    await area.get("form").trigger("submit");
    expect(f.events()).toHaveLength(1);
    expect(area.get('[role="alert"]').text()).toContain("URL");
    await prepare(f.wrapper, "  https://merchant-refund201.invalid/new  ");
    expect(f.events()).toHaveLength(1);
    await confirm(f.wrapper);
    expect(
      f.events().map((call) => ({ method: call.method, body: call.body })),
    ).toEqual([
      { method: "GET", body: undefined },
      {
        method: "PATCH",
        body: {
          event_url: "https://merchant-refund201.invalid/new",
          event_types: ["refund"],
          expected_fingerprint: "a".repeat(64),
        },
      },
    ]);
    expect(f.events()[1].headers.get("X-CSRF-Token")).toBe(
      "refund201-own-session",
    );
    expect(panel(f.wrapper).text()).toContain("订阅设置已保存");
    expect(panel(f.wrapper).text()).not.toMatch(
      /回调已送达|消息已送达|测试成功|已发送测试/,
    );
    expect(
      f.calls.every((call) =>
        call.path.startsWith("/console/v1/merchant/account/"),
      ),
    ).toBe(true);
  });

  it("requires an explicit disable confirmation and preserves the URL while clearing only event types", async () => {
    const f = await setup({
      reply: async (call) =>
        response(
          call.method === "PATCH"
            ? { ...initial, event_types: [], fingerprint: "b".repeat(64) }
            : initial,
        ),
    });
    const area = panel(f.wrapper);
    await button(area, "禁用订阅").trigger("click");
    expect(f.events()).toHaveLength(1);
    expect(area.get('[role="group"]').text()).toContain("禁用");
    await confirm(f.wrapper, "确认禁用");
    expect(f.events()[1].body).toEqual({
      event_url: callback,
      event_types: [],
      expected_fingerprint: initial.fingerprint,
    });
    expect(panel(f.wrapper).get('[name="event_url"]').element).toHaveProperty(
      "value",
      callback,
    );
    expect(panel(f.wrapper).get('[name="event_types"]').element).toHaveProperty(
      "checked",
      false,
    );
  });

  it("returns focus after Escape or cancellation without writing or discarding the edited URL", async () => {
    const f = await setup();
    const edited = "https://merchant-refund201.invalid/edited";
    await prepare(f.wrapper, edited);
    let area = panel(f.wrapper);
    expect(document.activeElement).toBe(button(area, "确认保存").element);
    await area.get('[role="group"]').trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(document.activeElement).toBe(button(area, "保存订阅").element);
    expect(area.get('[name="event_url"]').element).toHaveProperty(
      "value",
      edited,
    );
    await area.get("form").trigger("submit");
    await button(area, "返回修改").trigger("click");
    expect(f.events()).toHaveLength(1);
    await button(area, "取消修改").trigger("click");
    area = panel(f.wrapper);
    expect(area.get('[name="event_url"]').element).toHaveProperty(
      "value",
      callback,
    );
  });

  it("shows the server's automatic cancellation notice and retained URL as read-only delivery facts", async () => {
    const f = await setup({
      state: {
        ...initial,
        event_types: [],
        last_delivery_state: "failed",
        subscription_notice:
          "连续发送失败10次，事件订阅已关闭，请检查订阅地址后重新开启。",
      },
    });
    const area = panel(f.wrapper);
    expect(area.text()).toContain("连续发送失败10次");
    expect(area.text()).toContain("事件订阅已关闭");
    expect(area.get('[name="event_url"]').element).toHaveProperty(
      "value",
      callback,
    );
    expect(area.get('[name="event_types"]').element).toHaveProperty(
      "checked",
      false,
    );
    expect(area.find('[name="last_delivery_state"]').exists()).toBe(false);
    expect(area.find('[name="subscription_notice"]').exists()).toBe(false);
    expect(f.events()).toHaveLength(1);
  });

  it("blocks duplicate writes while a save is pending and keeps callback delivery distinct from saving", async () => {
    const pending = deferred<Response>();
    const f = await setup({
      reply: (call) =>
        call.method === "PATCH"
          ? pending.promise
          : Promise.resolve(response(initial)),
    });
    await prepare(f.wrapper);
    await button(panel(f.wrapper), "确认保存").trigger("click");
    await panel(f.wrapper).get("form").trigger("submit");
    expect(
      button(panel(f.wrapper), "重新读取订阅").attributes("disabled"),
    ).toBeDefined();
    expect(f.events().filter((call) => call.method === "PATCH")).toHaveLength(
      1,
    );
    pending.resolve(
      response({
        ...initial,
        fingerprint: "b".repeat(64),
        last_delivery_state: "none",
      }),
    );
    await flushPromises();
    expect(panel(f.wrapper).text()).not.toMatch(/回调已送达|消息已送达/);
  });

  it("keeps a decoded validation rejection editable and never contacts the callback URL", async () => {
    const f = await setup({
      reply: async (call) =>
        call.method === "PATCH"
          ? response(null, 422, "订阅URL不允许使用内网地址")
          : response(initial),
    });
    await prepare(f.wrapper, "https://merchant-refund201.invalid/rejected");
    await confirm(f.wrapper);
    const area = panel(f.wrapper);
    expect(area.get('[role="alert"]').text()).toContain(
      "订阅URL不允许使用内网地址",
    );
    expect(area.get('[name="event_url"]').element).toHaveProperty(
      "value",
      "https://merchant-refund201.invalid/rejected",
    );
    expect(f.events().filter((call) => call.method === "PATCH")).toHaveLength(
      1,
    );
    expect(f.calls.some((call) => call.path.startsWith("https://"))).toBe(
      false,
    );
  });

  it.each([
    "ftp://merchant-refund201.invalid/events",
    "https://user:password@merchant-refund201.invalid/events",
    "https://merchant-refund201.invalid/events#fragment",
    "https://merchant-refund201.invalid/" + "x".repeat(500),
  ])(
    "rejects an invalid callback draft without preparing or sending a write: %s",
    async (url) => {
      const f = await setup();
      await prepare(f.wrapper, url);
      const area = panel(f.wrapper);
      expect(area.find('[role="group"]').exists()).toBe(false);
      expect(area.get('[role="alert"]').text()).toMatch(/URL|地址|500/);
      expect(f.events().map((call) => call.method)).toEqual(["GET"]);
      expect(f.calls.some((call) => call.path.startsWith("https://"))).toBe(
        false,
      );
    },
  );

  it.each(["conflict", "network", "malformed", "mismatched-result"] as const)(
    "requires an explicit read-only recheck after %s and never resubmits automatically",
    async (mode) => {
      let reads = 0;
      const f = await setup({
        reply: async (call) => {
          if (call.method === "GET") {
            reads++;
            return response(
              reads === 1
                ? initial
                : {
                    ...initial,
                    event_url: "https://merchant-refund201.invalid/concurrent",
                    fingerprint: "c".repeat(64),
                  },
            );
          }
          if (mode === "conflict") return response(null, 409, "changed");
          if (mode === "network")
            throw new TypeError("synthetic network interruption201");
          if (mode === "malformed")
            return response({ ...initial, event_types: ["complain"] });
          return response({
            ...initial,
            event_url: "https://merchant-refund201.invalid/unexpected",
            fingerprint: "b".repeat(64),
          });
        },
      });
      await prepare(f.wrapper, "https://merchant-refund201.invalid/edited");
      await confirm(f.wrapper);
      let area = panel(f.wrapper);
      expect(area.text()).toContain("重新读取");
      expect(area.text()).not.toContain("订阅设置已保存");
      expect(area.find("form").exists()).toBe(false);
      expect(f.events()).toHaveLength(2);
      await button(area, "重新读取订阅").trigger("click");
      await flushPromises();
      area = panel(f.wrapper);
      expect(f.events().map((call) => call.method)).toEqual([
        "GET",
        "PATCH",
        "GET",
      ]);
      expect(area.get('[name="event_url"]').element).toHaveProperty(
        "value",
        "https://merchant-refund201.invalid/concurrent",
      );
    },
  );

  it.each([
    null,
    { ...initial, event_url: 201 },
    { ...initial, event_types: ["chargeback"] },
    { ...initial, event_types: ["refund", "refund"] },
    { ...initial, fingerprint: "A".repeat(64) },
    { ...initial, consecutive_failures: -1 },
    { ...initial, consecutive_failures: 10 },
    { ...initial, consecutive_failures: "1" },
    { ...initial, last_delivery_state: "success-ish" },
    { ...initial, subscription_notice: "x".repeat(1001) },
    { ...initial, provider_token: "unexpected201" },
  ])(
    "does not enable editing for malformed subscription data: %j",
    async (state) => {
      // Explicit null must not be replaced by the default in setup.
      const f = await setup({ reply: async () => response(state) });
      const area = panel(f.wrapper);
      expect(area.find("form").exists()).toBe(false);
      expect(area.get('[role="alert"]').text()).toMatch(/读取|格式|核对/);
      expect(f.events().map((call) => call.method)).toEqual(["GET"]);
    },
  );

  it("keeps drafts, callback addresses, fingerprints and notices out of browser storage", async () => {
    const local = vi.spyOn(Storage.prototype, "setItem");
    const f = await setup();
    await prepare(
      f.wrapper,
      "https://merchant-refund201.invalid/private-draft",
    );
    await button(panel(f.wrapper), "返回修改").trigger("click");
    expect(local).not.toHaveBeenCalled();
    expect(document.cookie).not.toContain("private-draft");
  });

  it("never reads subscription data in a delegated merchant session", async () => {
    const f = await setup({ ownSession: false });
    const area = panel(f.wrapper);
    expect(area.text()).toContain("本人");
    expect(area.find("form").exists()).toBe(false);
    expect(f.events()).toEqual([]);
  });
});

const contextChanges = [
  "client",
  "csrf",
  "merchant",
  "own-session",
  "section",
  "unmount",
] as const;
async function changeContext(
  f: Awaited<ReturnType<typeof setup>>,
  change: (typeof contextChanges)[number],
) {
  if (change === "client") {
    const client = newClient();
    client.csrfToken = "refund201-replacement-session";
    await f.wrapper.setProps({ client });
  } else if (change === "csrf")
    f.client.csrfToken = "refund201-replacement-session";
  else if (change === "merchant")
    await f.wrapper.setProps({ merchantId: "9007199254740995" } as never);
  else if (change === "own-session")
    await f.wrapper.setProps({ ownSession: false } as never);
  else if (change === "section")
    await f.wrapper.setProps({ section: "profile" });
  else {
    f.wrapper.unmount();
    mounted.splice(mounted.indexOf(f.wrapper), 1);
  }
  await flushPromises();
}

describe.each(contextChanges)(
  "refund subscription after %s context change",
  (change) => {
    it("aborts an old read and never installs its URL or notice on the replacement screen", async () => {
      const pending = deferred<Response>();
      let reads = 0;
      const f = await setup({
        reply: (call) =>
          call.method === "GET" && ++reads === 1
            ? pending.promise
            : Promise.resolve(
                response({
                  ...initial,
                  event_url: "https://merchant-refund201.invalid/next-session",
                  fingerprint: "b".repeat(64),
                }),
              ),
      });
      // This assertion makes the pre-feature failure an absent mounted feature, not an undefined signal error.
      panel(f.wrapper);
      expect(f.events()).toHaveLength(1);
      await changeContext(f, change);
      expect(f.events()[0].signal?.aborted).toBe(true);
      pending.resolve(
        response({
          ...initial,
          event_url: "https://merchant-refund201.invalid/old-session",
          subscription_notice: "old-context-notice201",
        }),
      );
      await flushPromises();
      expect(document.body.textContent).not.toContain("old-context-notice201");
      expect(document.body.innerHTML).not.toContain("old-session");
      expect(f.events().every((call) => call.method === "GET")).toBe(true);
    });

    it("aborts a pending write and never reports its late success in the replacement context", async () => {
      const pending = deferred<Response>();
      const f = await setup({
        reply: (call) =>
          call.method === "PATCH"
            ? pending.promise
            : Promise.resolve(response(initial)),
      });
      await prepare(f.wrapper, "https://merchant-refund201.invalid/old-draft");
      await button(panel(f.wrapper), "确认保存").trigger("click");
      const write = f.events().find((call) => call.method === "PATCH");
      expect(write).toBeDefined();
      await changeContext(f, change);
      expect(write!.signal?.aborted).toBe(true);
      pending.resolve(
        response({
          ...initial,
          event_url: "https://merchant-refund201.invalid/old-draft",
          fingerprint: "b".repeat(64),
          subscription_notice: "late-write-notice201",
        }),
      );
      await flushPromises();
      expect(document.body.textContent).not.toContain("订阅设置已保存");
      expect(document.body.textContent).not.toContain("late-write-notice201");
      expect(document.body.innerHTML).not.toContain("old-draft");
      expect(f.events().filter((call) => call.method === "PATCH")).toHaveLength(
        1,
      );
    });
  },
);

it("rejects late responses and old drafts when an unobserved raw ApiClient CSRF changes", async () => {
  const pending = deferred<Response>();
  let reads = 0;
  const f = await setup({
    reactiveClient: false,
    reply: () =>
      ++reads === 1 ? pending.promise : Promise.resolve(response(initial)),
  });
  panel(f.wrapper);
  f.client.csrfToken = "refund201-unobserved-session";
  pending.resolve(
    response({ ...initial, subscription_notice: "old-raw-context201" }),
  );
  await flushPromises();
  expect(panel(f.wrapper).text()).not.toContain("old-raw-context201");
  expect(panel(f.wrapper).find("form").exists()).toBe(false);
  await button(panel(f.wrapper), "重新读取订阅").trigger("click");
  await flushPromises();
  f.client.csrfToken = "refund201-second-unobserved-session";
  await panel(f.wrapper).get("form").trigger("submit");
  await flushPromises();
  expect(f.events().filter((call) => call.method === "PATCH")).toHaveLength(0);
  expect(panel(f.wrapper).find('[role="group"]').exists()).toBe(false);
});
