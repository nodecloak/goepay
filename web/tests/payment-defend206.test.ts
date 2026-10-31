// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import {
  DOMWrapper,
  flushPromises,
  mount,
  type VueWrapper,
} from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import SettingsPage from "../src/components/SettingsPage.vue";
import { readPaymentDefend206Settings } from "../src/payment-defend206";

const endpoint = "/console/v1/admin/settings/payment-defend";
const keys = [
  "pay_verify",
  "pay_verify_type",
  "pay_verify_check_second",
  "pay_verify_check_count",
  "pay_verify_check_rate",
  "pay_verify_check_ip",
  "pay_verify_check_uid",
] as const;
type Settings = Record<(typeof keys)[number], string>;
const defaults: Settings = {
  pay_verify: "0",
  pay_verify_type: "0",
  pay_verify_check_second: "",
  pay_verify_check_count: "",
  pay_verify_check_rate: "",
  pay_verify_check_ip: "",
  pay_verify_check_uid: "",
};
const positive: Settings = {
  pay_verify: "3",
  pay_verify_type: "3",
  pay_verify_check_second: "60",
  pay_verify_check_count: "10",
  pay_verify_check_rate: "30",
  pay_verify_check_ip: "0",
  pay_verify_check_uid: "",
};
function view(
  settings: Settings = defaults,
  revision = "0",
  fingerprint = "a".repeat(64),
) {
  return {
    settings: { ...settings },
    configured: revision !== "0",
    revision,
    fingerprint,
    can_update: true,
  };
}
type Call = {
  path: string;
  method: string;
  body: unknown;
  csrf: string | null;
  signal?: AbortSignal | null;
};
type Options = {
  state?: unknown;
  ownSession?: boolean;
  adminClass?: string;
  csrf?: string;
  area?: "admin" | "merchant";
  handler?: (call: Call) => Promise<Response>;
};
const mounted: VueWrapper[] = [],
  unexpected: string[][] = [];
function response(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({
      code: status === 200 ? 0 : -1,
      message:
        status === 409
          ? "policy changed206"
          : status === 200
            ? ""
            : "independent setting unavailable206",
      data,
    }),
    { status },
  );
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
function client(
  csrf = "original-payment-defend206",
  area: "admin" | "merchant" = "admin",
) {
  const value = reactive(new ApiClient(area));
  value.csrfToken = csrf;
  return value;
}
async function setup(options: Options = {}) {
  const calls: Call[] = [],
    rejected: string[] = [];
  unexpected.push(rejected);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const call: Call = {
        path: String(input),
        method: init?.method ?? "GET",
        body:
          init?.body === undefined ? undefined : JSON.parse(String(init.body)),
        csrf: new Headers(init?.headers).get("X-CSRF-Token"),
        signal: init?.signal,
      };
      calls.push(call);
      if (
        call.path === endpoint &&
        (call.method === "GET" || call.method === "PATCH")
      ) {
        if (options.handler) return options.handler(call);
        if (call.method === "PATCH")
          return response(
            view(
              (call.body as { settings: Settings }).settings,
              "1",
              "b".repeat(64),
            ),
          );
        return response(options.state ?? view());
      }
      if (
        call.method === "GET" &&
        /^\/console\/v1\/(admin|merchant)\/resources\/settings\/schema$/.test(
          call.path,
        )
      )
        return response({
          title: "系统设置",
          fields: [{ name: "key", type: "select", options: [] }],
          permissions: { create: true, update: true, delete: false },
        });
      if (
        call.method === "GET" &&
        /^\/console\/v1\/(admin|merchant)\/resources\/settings\?page=1&limit=100$/.test(
          call.path,
        )
      )
        return response({ items: [], total: 0 });
      // These are existing independent cards mounted by SettingsPage. Their
      // explicit 503 responses cannot grant authority or fake future methods.
      if (
        call.method === "GET" &&
        /^\/console\/v1\/(admin|merchant)\/(settings\/(branding|captcha|merchant-access)|payment-risk(?:\/actions)?)$/.test(
          call.path,
        )
      )
        return response(null, 503);
      rejected.push(`${call.method} ${call.path}`);
      return response(null, 503);
    }),
  );
  const api = client(
    options.csrf ?? "original-payment-defend206",
    options.area ?? "admin",
  );
  const wrapper = mount(SettingsPage, {
    attachTo: document.body,
    props: {
      client: api,
      adminClass: options.adminClass ?? "super",
      ownSession: options.ownSession ?? true,
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  await wrapper.get("#settings-tab-risk").trigger("click");
  await flushPromises();
  return {
    wrapper,
    api,
    calls,
    reads: () =>
      calls.filter((call) => call.path === endpoint && call.method === "GET"),
    writes: () => calls.filter((call) => call.method !== "GET"),
  };
}
function panel(wrapper: VueWrapper) {
  const found = wrapper.find('section[aria-label="创建订单人机验证"]');
  expect(
    found.exists(),
    "real SettingsPage risk tab must mount an independent original payment verification card",
  ).toBe(true);
  return found;
}
function button(scope: Pick<DOMWrapper<Element>, "findAll">, pattern: RegExp) {
  const found = scope
    .findAll("button")
    .find((entry) => pattern.test(entry.text()));
  expect(found, `visible action ${pattern.source}`).toBeDefined();
  return found!;
}
function writable(scope: DOMWrapper<Element>) {
  return scope
    .findAll<HTMLInputElement | HTMLSelectElement>("input,select,textarea")
    .filter((field) => !field.element.disabled);
}
async function draft(wrapper: VueWrapper) {
  const area = panel(wrapper);
  await area.get('select[name="pay_verify"]').setValue("1");
  for (const key of [
    "pay_verify_check_second",
    "pay_verify_check_count",
    "pay_verify_check_rate",
    "pay_verify_check_ip",
    "pay_verify_check_uid",
  ] as const)
    await area.get(`[name="${key}"]`).setValue(positive[key]);
  await area.get('select[name="pay_verify_type"]').setValue("3");
  await area.get('select[name="pay_verify"]').setValue("3");
  return area;
}
async function prepare(wrapper: VueWrapper) {
  const area = await draft(wrapper);
  await button(area, /核对并保存/).trigger("click");
  await flushPromises();
  return wrapper.get('[role="dialog"]');
}
async function confirmLocalReread(wrapper: VueWrapper, reads: () => Call[]) {
  const before = reads().length,
    body = new DOMWrapper(document.body);
  const discard = () =>
    body.get('[role="dialog"][aria-label="放弃未保存的设置？"]');
  expect(discard().text()).toContain("创建订单人机验证");
  await button(discard(), /^继续编辑$/).trigger("click");
  await flushPromises();
  expect(body.find('[role="dialog"]').exists()).toBe(false);
  expect(reads()).toHaveLength(before);
  await button(panel(wrapper), /重新读取|刷新/).trigger("click");
  await flushPromises();
  expect(discard().text()).toContain("创建订单人机验证");
  await button(discard(), /^放弃修改并重新读取$/).trigger("click");
  await flushPromises();
  expect(reads()).toHaveLength(before + 1);
}
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  for (const calls of unexpected.splice(0))
    expect(
      calls,
      "unknown APIs must not be accepted by the payment-defend fixture",
    ).toEqual([]);
});

it("206 mounts the independent payment verification card in the original risk tab and reads its own five-field contract", async () => {
  const f = await setup(),
    area = panel(f.wrapper);
  expect(f.wrapper.get("#settings-tab-risk").attributes("aria-selected")).toBe(
    "true",
  );
  expect(f.reads()).toHaveLength(1);
  expect(f.writes()).toEqual([]);
  expect(area.get('select[name="pay_verify"]').element).toHaveProperty(
    "value",
    "0",
  );
  expect(area.text()).toContain("创建订单人机验证");
  expect(area.text()).toContain("人机验证类型");
  expect(area.text()).toContain("开启后无法通过mapi接口发起支付");
  expect(
    area
      .get('select[name="pay_verify"]')
      .findAll("option")
      .map((entry) => [entry.attributes("value"), entry.text()]),
  ).toEqual([
    ["0", "关闭"],
    ["1", "按规则开启"],
    ["2", "指定商户开启"],
    ["3", "全部商户开启"],
  ]);
  expect(
    area
      .get('select[name="pay_verify_type"]')
      .findAll("option")
      .map((entry) => [entry.attributes("value"), entry.text()]),
  ).toEqual([
    ["0", "JS跳转验证（速度快）"],
    ["1", "极验无感验证（速度慢）"],
    ["2", "极验滑动验证（速度慢且体验差）"],
    ["3", "禁止支付"],
  ]);
});

it("206 preserves the Pro field labels, placeholders and conditional visibility without asserting encrypted consumer semantics", async () => {
  const f = await setup(),
    area = panel(f.wrapper),
    mode = area.get('select[name="pay_verify"]');
  expect(area.find('[name="pay_verify_check_uid"]').exists()).toBe(false);
  await mode.setValue("1");
  expect(area.text()).toContain("人机验证开启规则1");
  expect(area.text()).toContain("人机验证开启规则2");
  expect(area.text()).toContain("开启人机验证的商户");
  expect(
    area.get('[name="pay_verify_check_second"]').attributes("placeholder"),
  ).toBe("检测商户多少秒内（不填写则不启用该条规则）");
  expect(
    area.get('[name="pay_verify_check_count"]').attributes("placeholder"),
  ).toBe("超过多少个订单时");
  expect(
    area.get('[name="pay_verify_check_rate"]').attributes("placeholder"),
  ).toBe("成功率低于多少时（不填写则不判断成功率）");
  expect(
    area.get('[name="pay_verify_check_ip"]').attributes("placeholder"),
  ).toBe("1小时内连续未支付订单数量（不填写则不启用该条规则）");
  expect(
    area.get('[name="pay_verify_check_uid"]').attributes("placeholder"),
  ).toBe("多个商户号用|隔开，留空为全部商户");
  // These assertions preserve visible Pro wording only. They deliberately do
  // not submit blank-rate mode1 or blank-UID mode2 as proven backend behavior.
  for (const value of ["2", "3"]) {
    await mode.setValue(value);
    expect(area.find('[name="pay_verify_check_second"]').exists()).toBe(false);
    expect(area.find('[name="pay_verify_check_ip"]').exists()).toBe(false);
    expect(area.find('[name="pay_verify_check_uid"]').exists()).toBe(true);
    expect(area.find('[name="pay_verify_type"]').exists()).toBe(true);
  }
  await mode.setValue("0");
  expect(area.find('[name="pay_verify_check_uid"]').exists()).toBe(false);
  expect(area.find('[name="pay_verify_type"]').exists()).toBe(true);
});

it("206 prepares a reviewable confirmation and cancelling it makes no PATCH and returns focus", async () => {
  const f = await setup(),
    dialog = await prepare(f.wrapper);
  expect(f.writes()).toEqual([]);
  expect(dialog.attributes("aria-modal")).toBe("true");
  expect(dialog.text()).toContain("全部商户开启");
  expect(dialog.text()).toContain("禁止支付");
  expect(dialog.text()).not.toContain("已创建订单");
  await button(dialog, /取消/).trigger("click");
  await flushPromises();
  expect(f.wrapper.find('[role="dialog"]').exists()).toBe(false);
  expect(f.writes()).toEqual([]);
  expect(document.activeElement).toBe(
    button(panel(f.wrapper), /核对并保存/).element,
  );
});

it("206 sends one real ApiClient PATCH with all seven original string fields and the exact fingerprint after confirmation", async () => {
  const storage = vi.spyOn(Storage.prototype, "setItem"),
    f = await setup(),
    dialog = await prepare(f.wrapper);
  await button(dialog, /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  expect(f.writes()[0]).toMatchObject({
    path: endpoint,
    method: "PATCH",
    csrf: "original-payment-defend206",
    body: { settings: positive, expected_fingerprint: "a".repeat(64) },
  });
  expect(Object.keys(f.writes()[0]!.body as object).sort()).toEqual([
    "expected_fingerprint",
    "settings",
  ]);
  expect(
    Object.keys(
      (f.writes()[0]!.body as { settings: Settings }).settings,
    ).sort(),
  ).toEqual([...keys].sort());
  expect(panel(f.wrapper).text()).toMatch(/已保存/);
  expect(panel(f.wrapper).text()).not.toMatch(
    /验证码验证成功|通道已连接|回调已送达|支付成功/,
  );
  expect(storage).not.toHaveBeenCalled();
});

it("206 failed initial reading stays closed until an explicit successful reread", async () => {
  let reads = 0;
  const f = await setup({
    handler: async () =>
      ++reads === 1 ? response(null, 503) : response(view()),
  });
  const area = panel(f.wrapper);
  expect(writable(area)).toEqual([]);
  expect(f.writes()).toEqual([]);
  expect(area.text()).toMatch(/无法|失败|不可用/);
  expect(f.reads()).toHaveLength(1);
  await button(area, /重新读取|刷新/).trigger("click");
  await flushPromises();
  expect(f.reads()).toHaveLength(2);
  expect(
    panel(f.wrapper).get('select[name="pay_verify"]').element,
  ).toHaveProperty("value", "0");
});

it.each([
  { ...view(), settings: { ...defaults, pay_verify: 0 } },
  { ...view(), settings: { ...defaults, unknown_rule: "1" } },
  { ...view(), settings: { pay_verify: "0", pay_verify_type: "0" } },
  { ...view(), revision: 0 },
  { ...view(), fingerprint: "short" },
  { ...view(), configured: true },
  { ...view(), can_update: false },
  { ...view(), extra: "untrusted" },
])(
  "206 malformed policy DTO never enables a fallback draft (%j)",
  async (state) => {
    const f = await setup({ state }),
      area = panel(f.wrapper);
    expect(writable(area)).toEqual([]);
    expect(f.writes()).toEqual([]);
    expect(area.text()).toMatch(/无法|失败|无效|核对/);
  },
);

it.each([
  { adminClass: "operator" },
  { ownSession: false },
  { csrf: "" },
  { area: "merchant" as const },
])(
  "206 authority is closed without an original super administrator context (%j)",
  async (options) => {
    const f = await setup(options),
      area = panel(f.wrapper);
    expect(f.reads()).toEqual([]);
    expect(writable(area)).toEqual([]);
    expect(f.writes()).toEqual([]);
    expect(area.text()).toMatch(/本人|超级管理员|权限|会话/);
  },
);

it("206 CAS409 requires rereading and a fresh confirmation with the new fingerprint", async () => {
  let reads = 0,
    writes = 0;
  const fresh = view({ ...positive, pay_verify: "0" }, "2", "c".repeat(64));
  const f = await setup({
    handler: async (call) =>
      call.method === "GET"
        ? response(++reads === 1 ? view() : fresh)
        : ++writes === 1
          ? response(null, 409)
          : response(view(positive, "3", "d".repeat(64))),
  });
  await button(await prepare(f.wrapper), /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  expect(panel(f.wrapper).text()).toMatch(/重新|刷新|变化|冲突/);
  await button(panel(f.wrapper), /重新读取|刷新/).trigger("click");
  await flushPromises();
  expect(f.reads()).toHaveLength(1);
  await confirmLocalReread(f.wrapper, f.reads);
  expect(
    panel(f.wrapper).get('select[name="pay_verify"]').element,
  ).toHaveProperty("value", "0");
  expect(f.writes()).toHaveLength(1);
  const dialog = await prepare(f.wrapper);
  expect(f.writes()).toHaveLength(1);
  await button(dialog, /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(2);
  expect(f.writes()[1]!.body).toEqual({
    settings: positive,
    expected_fingerprint: "c".repeat(64),
  });
});

it("206 an unknown PATCH outcome permits only a read before another write", async () => {
  let writes = 0;
  const f = await setup({
    handler: async (call) => {
      if (call.method === "PATCH") {
        writes++;
        throw new TypeError("controlled lost payment-defend response206");
      }
      return response(writes ? view(positive, "1", "b".repeat(64)) : view());
    },
  });
  await button(await prepare(f.wrapper), /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  const area = panel(f.wrapper);
  const save = area
    .findAll("button")
    .find((entry) => /核对并保存/.test(entry.text()));
  if (save) {
    expect(save.attributes("disabled")).toBeDefined();
    await save.trigger("click");
  }
  expect(f.writes()).toHaveLength(1);
  await button(area, /重新读取|刷新/).trigger("click");
  await flushPromises();
  expect(f.reads()).toHaveLength(1);
  await confirmLocalReread(f.wrapper, f.reads);
  expect(f.writes()).toHaveLength(1);
  expect(
    panel(f.wrapper).get('select[name="pay_verify"]').element,
  ).toHaveProperty("value", "3");
});

it.each(["client", "csrf"])(
  "206 late GET cannot populate a different %s context",
  async (change) => {
    const pending = deferred<Response>();
    let reads = 0;
    const f = await setup({
      handler: async () =>
        ++reads === 1
          ? pending.promise
          : response(view(defaults, "2", "c".repeat(64))),
    });
    panel(f.wrapper);
    if (change === "client")
      await f.wrapper.setProps({ client: client("new-payment-defend206") });
    else f.api.csrfToken = "new-payment-defend206";
    await flushPromises();
    pending.resolve(response(view(positive, "1", "b".repeat(64))));
    await flushPromises();
    expect(panel(f.wrapper).text()).not.toMatch(/已保存/);
    expect(f.writes()).toEqual([]);
    const field = panel(f.wrapper).find('select[name="pay_verify"]');
    if (field.exists()) expect(field.element).toHaveProperty("value", "0");
  },
);

it.each(["client", "csrf"])(
  "206 late PATCH result cannot report success or restore drafts in a different %s context",
  async (change) => {
    const pending = deferred<Response>();
    let reads = 0;
    const f = await setup({
      handler: async (call) =>
        call.method === "PATCH"
          ? pending.promise
          : response(
              ++reads === 1 ? view() : view(defaults, "2", "c".repeat(64)),
            ),
    });
    await button(await prepare(f.wrapper), /确认保存/).trigger("click");
    await flushPromises();
    expect(f.writes()).toHaveLength(1);
    if (change === "client")
      await f.wrapper.setProps({ client: client("new-payment-defend206") });
    else f.api.csrfToken = "new-payment-defend206";
    await flushPromises();
    pending.resolve(response(view(positive, "1", "b".repeat(64))));
    await flushPromises();
    const area = panel(f.wrapper);
    expect(area.text()).not.toMatch(/已保存/);
    const field = area.find('select[name="pay_verify"]');
    if (field.exists()) expect(field.element).toHaveProperty("value", "0");
    expect(f.writes()).toHaveLength(1);
    expect(f.writes()[0]?.csrf).toBe("original-payment-defend206");
  },
);

it("206 revoking authority while confirmation is open invalidates it without writing", async () => {
  const f = await setup();
  await prepare(f.wrapper);
  await f.wrapper.setProps({ ownSession: false });
  await flushPromises();
  expect(f.wrapper.find('[role="dialog"]').exists()).toBe(false);
  expect(writable(panel(f.wrapper))).toEqual([]);
  expect(f.writes()).toEqual([]);
});

it("206 Escape cancels the actual confirmation and never submits its pending policy", async () => {
  const f = await setup(),
    dialog = await prepare(f.wrapper);
  await dialog.trigger("keydown", { key: "Escape" });
  await flushPromises();
  expect(f.wrapper.find('[role="dialog"]').exists()).toBe(false);
  expect(f.writes()).toEqual([]);
  expect(document.activeElement).toBe(
    button(panel(f.wrapper), /核对并保存/).element,
  );
});

it("206 unmount discards a late PATCH and writes no browser storage or payment requests", async () => {
  const pending = deferred<Response>(),
    storage = vi.spyOn(Storage.prototype, "setItem");
  const f = await setup({
    handler: async (call) =>
      call.method === "PATCH" ? pending.promise : response(view()),
  });
  await button(await prepare(f.wrapper), /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  f.wrapper.unmount();
  pending.resolve(response(view(positive, "1", "b".repeat(64))));
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  expect(storage).not.toHaveBeenCalled();
  expect(f.calls.every((call) => call.path.startsWith("/console/v1/"))).toBe(
    true,
  );
});

it("206 reads and round-trips the original leading-zero, long-decimal and duplicate-UID administrator text", async () => {
  const original: Settings = {
    ...positive,
    pay_verify: "1",
    pay_verify_check_second: "00060",
    pay_verify_check_count: "0005",
    pay_verify_check_rate: "00099.999999999999999999999999999999999999",
    pay_verify_check_ip: "0002",
    pay_verify_check_uid: "0001|9223372036854775807|0001",
  };
  const f = await setup({
    handler: async (call) =>
      call.method === "GET"
        ? response(view(original, "1"))
        : response(
            view(
              (call.body as { settings: Settings }).settings,
              "2",
              "b".repeat(64),
            ),
          ),
  });
  const area = panel(f.wrapper);
  for (const key of keys)
    expect(area.get(`[name="${key}"]`).element).toHaveProperty(
      "value",
      original[key],
    );
  await area.get('[name="pay_verify"]').setValue("3");
  await button(area, /核对并保存/).trigger("click");
  await flushPromises();
  const dialog = f.wrapper.get('[role="dialog"]');
  expect(dialog.text()).toContain(original.pay_verify_check_rate);
  expect(dialog.text()).toContain(original.pay_verify_check_uid);
  await button(dialog, /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  expect(f.writes()[0]!.body).toEqual({
    settings: { ...original, pay_verify: "3" },
    expected_fingerprint: "a".repeat(64),
  });
  expect(panel(f.wrapper).text()).toMatch(/已保存/);
  await panel(f.wrapper).get('[name="pay_verify"]').setValue("1");
  for (const key of keys.filter((key) => key !== "pay_verify"))
    expect(panel(f.wrapper).get(`[name="${key}"]`).element).toHaveProperty(
      "value",
      original[key],
    );
});

it("206 permits backend-valid wide numeric text and more than 128 UID entries without conversion or deduplication", async () => {
  const raw: Settings = {
    ...positive,
    pay_verify_check_second: "00031536001",
    pay_verify_check_count: "0001000001",
    pay_verify_check_ip: "0001000001",
    pay_verify_check_rate: "00123.4567890123456789012345678901234567890",
    pay_verify_check_uid:
      Array.from({ length: 129 }, () => "0001").join("|") +
      "|9223372036854775807|0001",
  };
  const f = await setup(),
    area = panel(f.wrapper);
  await area.get('[name="pay_verify"]').setValue("1");
  for (const key of [
    "pay_verify_check_second",
    "pay_verify_check_count",
    "pay_verify_check_rate",
    "pay_verify_check_ip",
  ] as const) {
    expect(
      area.get<HTMLInputElement>(`[name="${key}"]`).element.maxLength,
    ).toBe(128);
    await area.get(`[name="${key}"]`).setValue(raw[key]);
  }
  await area
    .get('[name="pay_verify_check_uid"]')
    .setValue(raw.pay_verify_check_uid);
  await area.get('[name="pay_verify_type"]').setValue("3");
  await area.get('[name="pay_verify"]').setValue("3");
  await button(area, /核对并保存/).trigger("click");
  await flushPromises();
  await button(f.wrapper.get('[role="dialog"]'), /确认保存/).trigger("click");
  await flushPromises();
  expect(f.writes()).toHaveLength(1);
  expect(f.writes()[0]!.body).toEqual({
    settings: raw,
    expected_fingerprint: "a".repeat(64),
  });
  expect(panel(f.wrapper).text()).toMatch(/已保存/);
});

it("206 preserves the backend 128-byte numeric and 8192-byte UID boundaries exactly", () => {
  const raw: Settings = {
    ...positive,
    pay_verify_check_second: "0".repeat(127) + "1",
    pay_verify_check_count: "9".repeat(128),
    pay_verify_check_ip: "0".repeat(128),
    pay_verify_check_rate: "0." + "9".repeat(126),
    pay_verify_check_uid: "0".repeat(8191) + "1",
  };
  expect(readPaymentDefend206Settings(raw)).toEqual(raw);
  for (const key of [
    "pay_verify_check_second",
    "pay_verify_check_count",
    "pay_verify_check_ip",
    "pay_verify_check_rate",
    "pay_verify_check_uid",
  ] as const) {
    expect(() =>
      readPaymentDefend206Settings({ ...raw, [key]: raw[key] + "0" }),
    ).toThrow();
  }
});

it.each([
  ["pay_verify_check_second", "-1"],
  ["pay_verify_check_count", "1e3"],
  ["pay_verify_check_ip", "127.0.0.1"],
  ["pay_verify_check_rate", "NaN"],
  ["pay_verify_check_rate", "1e2"],
  ["pay_verify_check_rate", ".5"],
  ["pay_verify_check_rate", "1."],
  ["pay_verify_check_second", " 60"],
  ["pay_verify_check_uid", "0"],
  ["pay_verify_check_uid", "9223372036854775808"],
  ["pay_verify_check_uid", "1||2"],
  ["pay_verify_check_uid", "1,2"],
  ["pay_verify_check_uid", "1| 2"],
  ["pay_verify", "03"],
])(
  "206 text compatibility still rejects backend-invalid %s=%s",
  (key, value) => {
    expect(() =>
      readPaymentDefend206Settings({ ...positive, [key!]: value }),
    ).toThrow();
  },
);
