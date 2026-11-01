// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient, ApiError } from "../src/api";
import Panel from "../src/components/SettlementRecipientPanel.vue";

const recipient = (id = "42", masked = "***1234") => ({
  state: "configured",
  config_id: id,
  type: "alipay",
  app_id: "",
  account_masked: masked,
  name_masked: "***",
});
const readableRecipient = (id = "42", masked = "***1234") => ({
  ...recipient(id, masked),
  can_write: true,
});
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  vi.restoreAllMocks();
});
const deferred = <T = unknown>() => {
  let resolve!: (value: T) => void, reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
const button = (wrapper: VueWrapper, label: string) =>
  wrapper.findAll("button").find((node) => node.text() === label)!;
async function fixture(
  options: {
    admin?: boolean;
    types?: unknown;
    failTypes?: boolean;
    post?: () => Promise<unknown>;
  } = {},
) {
  const client = reactive(new ApiClient(options.admin ? "admin" : "merchant"));
  client.csrfToken = "synthetic-csrf";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, init) => {
      if (init?.method === "POST")
        return options.post ? options.post() : recipient("43");
      if (path.includes("allowed-types")) {
        if (options.failTypes)
          throw new ApiError("preview unavailable", 422, -1);
        return options.types === undefined
          ? { settlement: ["alipay", "wxpay", "bank"], can_write: true }
          : { ...(options.types as Record<string, unknown>), can_write: true };
      }
      return readableRecipient();
    });
  const wrapper = mount(Panel, {
    props: {
      client,
      admin: !!options.admin,
      merchantID: options.admin ? "101" : undefined,
      disabled: false,
    },
  });
  mounted.push(wrapper);
  await flushPromises();
  return { wrapper, client, request };
}
async function editAndFill(wrapper: VueWrapper) {
  await button(wrapper, "修改收款资料").trigger("click");
  await wrapper
    .get('input[aria-label="收款账号"]')
    .setValue("synthetic-private-account");
  await wrapper
    .get('input[aria-label="真实姓名"]')
    .setValue("Synthetic Private Name");
}

it.each([
  { settlement: [] },
  { settlement: ["qqpay"] },
  {},
  { settlement: "alipay" },
])(
  "fails closed for empty or unsupported/malformed rail lists while keeping masked history: %j",
  async (types) => {
    const { wrapper, request } = await fixture({ types });
    expect(wrapper.text()).toContain("***1234");
    expect(
      button(wrapper, "修改收款资料").attributes("disabled"),
    ).toBeDefined();
    await button(wrapper, "修改收款资料").trigger("click");
    expect(wrapper.find('input[aria-label="收款账号"]').exists()).toBe(false);
    expect(button(wrapper, "刷新收款版本")).toBeTruthy();
    expect(
      request.mock.calls.filter(([, init]) => init?.method === "POST"),
    ).toHaveLength(0);
  },
);

it("keeps masked configuration and blocks edits when the admin's selected merchant preview is unavailable", async () => {
  const { wrapper, request } = await fixture({ admin: true, failTypes: true });
  expect(
    request.mock.calls.some(
      ([path]) =>
        path ===
        "/workflows/settlement-recipient/allowed-types?merchant_id=101",
    ),
  ).toBe(true);
  expect(wrapper.text()).toContain("***1234");
  expect(wrapper.text()).toContain("暂不可编辑");
  expect(button(wrapper, "修改收款资料").attributes("disabled")).toBeDefined();
});

it("uses no merchant query for the authenticated merchant's allowed rails", async () => {
  const { request } = await fixture();
  expect(
    request.mock.calls.some(
      ([path]) => path === "/workflows/settlement-recipient/allowed-types",
    ),
  ).toBe(true);
});

it.each([
  new TypeError("network outcome unknown"),
  new ApiError("server outcome unknown", 503, -1),
  { unexpected: "saved response" },
])(
  "clears private drafts and locks repeated writes until explicit successful readback: %j",
  async (outcome) => {
    let posts = 0,
      reads = 0;
    const { wrapper, request } = await fixture({
      post: async () => {
        posts++;
        if (outcome instanceof Error) throw outcome;
        return outcome;
      },
    });
    request.mockImplementation(async (path, init) => {
      if (init?.method === "POST") {
        posts++;
        if (outcome instanceof Error) throw outcome;
        return outcome;
      }
      if (path.includes("allowed-types"))
        return { settlement: ["alipay"], can_write: true };
      reads++;
      return readableRecipient("43");
    });
    await editAndFill(wrapper);
    await button(wrapper, "保存收款版本").trigger("click");
    await flushPromises();
    expect(posts).toBe(1);
    expect(wrapper.text()).toContain("保存结果尚未确认");
    expect(wrapper.html()).not.toContain("synthetic-private-account");
    expect(wrapper.html()).not.toContain("Synthetic Private Name");
    expect(wrapper.find('input[aria-label="收款账号"]').exists()).toBe(false);
    expect(
      button(wrapper, "修改收款资料").attributes("disabled"),
    ).toBeDefined();
    expect(reads).toBe(0);
    await button(wrapper, "刷新收款版本").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("版本 43");
    expect(
      button(wrapper, "修改收款资料").attributes("disabled"),
    ).toBeUndefined();
    expect(posts).toBe(1);
  },
);

it("keeps a 422 draft repairable and reads a 409 conflict back without another write", async () => {
  const { wrapper, request } = await fixture({
    post: async () => {
      throw new ApiError("recipient validation rejected", 422, -1);
    },
  });
  await editAndFill(wrapper);
  await button(wrapper, "保存收款版本").trigger("click");
  await flushPromises();
  expect(
    (wrapper.get('input[aria-label="收款账号"]').element as HTMLInputElement)
      .value,
  ).toBe("synthetic-private-account");
  request.mockImplementation(async (path, init) => {
    if (init?.method === "POST")
      throw new ApiError("version conflict", 409, -1);
    return path.includes("allowed-types")
      ? { settlement: ["alipay"], can_write: true }
      : readableRecipient("44");
  });
  await button(wrapper, "保存收款版本").trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("版本 44");
  expect(wrapper.text()).toContain("版本已变化");
  expect(wrapper.html()).not.toContain("synthetic-private-account");
});

it("keeps unknown-write recovery locked when explicit readback fails", async () => {
  const { wrapper, request } = await fixture({
    post: async () => {
      throw new TypeError("unknown write");
    },
  });
  await editAndFill(wrapper);
  await button(wrapper, "保存收款版本").trigger("click");
  await flushPromises();
  request.mockRejectedValue(new ApiError("read unavailable", 503, -1));
  await button(wrapper, "刷新收款版本").trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("***1234");
  expect(wrapper.text()).toContain("保存结果尚未确认");
  expect(button(wrapper, "修改收款资料").attributes("disabled")).toBeDefined();
  expect(
    button(wrapper, "刷新收款版本").attributes("disabled"),
  ).toBeUndefined();
});

it("invalidates drafts and reloads when client, CSRF or role area changes", async () => {
  const { wrapper, client, request } = await fixture();
  await editAndFill(wrapper);
  client.csrfToken = "new-session";
  await flushPromises();
  expect(wrapper.find('input[aria-label="收款账号"]').exists()).toBe(false);
  expect(
    request.mock.calls.filter(
      ([path]) => path === "/workflows/settlement-recipient",
    ),
  ).toHaveLength(2);
  client.area = "admin";
  await flushPromises();
  expect(
    request.mock.calls.filter(
      ([path]) => path === "/workflows/settlement-recipient",
    ),
  ).toHaveLength(3);
  const replacement = new ApiClient("merchant");
  replacement.csrfToken = "replacement";
  const next = vi
    .spyOn(replacement, "request")
    .mockImplementation(async (path) =>
      path.includes("allowed-types")
        ? { settlement: ["alipay"], can_write: true }
        : readableRecipient("45"),
    );
  await wrapper.setProps({ client: replacement });
  await flushPromises();
  expect(next).toHaveBeenCalled();
  expect(wrapper.text()).toContain("版本 45");
});

it("ignores old save/finally responses after context changes and does not unlock a new save", async () => {
  const oldSave = deferred(),
    newSave = deferred();
  const { wrapper, request } = await fixture({ admin: true });
  let posts = 0;
  request.mockImplementation(async (path, init) => {
    if (init?.method === "POST")
      return ++posts === 1 ? oldSave.promise : newSave.promise;
    return path.includes("allowed-types")
      ? { settlement: ["alipay"], can_write: true }
      : readableRecipient(path.endsWith("=202") ? "52" : "42");
  });
  await editAndFill(wrapper);
  await button(wrapper, "保存收款版本").trigger("click");
  await wrapper.setProps({ merchantID: "202" });
  await flushPromises();
  expect(wrapper.get("fieldset").attributes("disabled")).toBeUndefined();
  expect(wrapper.text()).toContain("版本 52");
  await editAndFill(wrapper);
  await button(wrapper, "保存收款版本").trigger("click");
  oldSave.resolve(recipient("43", "***old-save"));
  await flushPromises();
  expect(wrapper.get("fieldset").attributes("disabled")).toBeDefined();
  expect(wrapper.text()).not.toContain("***old-save");
  newSave.resolve(recipient("53"));
  await flushPromises();
  expect(wrapper.text()).toContain("版本 53");
});

it("does not apply a late recipient read or request its rails after the session changes", async () => {
  const previousRead = deferred();
  const { wrapper, client, request } = await fixture();
  let reads = 0,
    railReads = 0;
  request.mockImplementation(async (path) => {
    if (path.includes("allowed-types")) {
      railReads++;
      return { settlement: ["alipay"], can_write: true };
    }
    return ++reads === 1
      ? previousRead.promise
      : readableRecipient("54", "***new-session");
  });
  client.csrfToken = "pending-session";
  await flushPromises();
  client.csrfToken = "current-session";
  await flushPromises();
  expect(wrapper.text()).toContain("版本 54");
  previousRead.resolve(readableRecipient("43", "***old-session"));
  await flushPromises();
  expect(wrapper.text()).not.toContain("***old-session");
  expect(wrapper.text()).toContain("***new-session");
  expect(railReads).toBe(1);
});
