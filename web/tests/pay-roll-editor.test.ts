import { pageReload248 } from "./helpers/page-reload248";
// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient, ApiError } from "../src/api";
import CreateForm from "../src/components/PayRollCreateForm.vue";
import ChannelEditor from "../src/components/PayRollChannelEditor.vue";

const catalog = {
  available: true,
  items: [
    { id: "7", type: "alipay", methods: ["page"] },
    { id: "8", type: "alipay", methods: ["page"] },
    { id: "9", type: "wxpay", methods: ["native"] },
  ],
};
const record = (revision = "a".repeat(64), enabled = false) => ({
  id: "41",
  revision,
  fields: {
    name: "轮询甲",
    type: "alipay",
    strategy: "weighted",
    merchant_id: "101",
    enabled,
    default_route: true,
    channel_ids: "7:2",
    limits: '{"daily_count":7}',
    rules: '{"7":{"daily_count":2}}',
  },
});
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  vi.restoreAllMocks();
});
const button = (w: VueWrapper, label: string) =>
  w.findAll("button").find((b) => b.text() === label)!;
const client = () => {
  const c = reactive(new ApiClient("admin"));
  c.csrfToken = "synthetic";
  return c;
};

it("creates only a named inert draft through the dedicated endpoint with the three visible fields", async () => {
  const c = client(),
    sent: unknown[] = [];
  vi.spyOn(c, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST") {
      sent.push({ path, body: init.body });
      return {
        id: "42",
        name: "新轮询",
        type: "alipay",
        strategy: "weighted",
        merchant_id: "0",
        enabled: false,
        default_route: false,
        channel_ids: "",
        limits: "{}",
        rules: "{}",
      };
    }
    return catalog;
  });
  let w = mount(CreateForm, { props: { client: c } });
  mounted.push(w);
  await flushPromises();
  expect(w.findAll("input")).toHaveLength(1);
  expect(w.findAll("select")).toHaveLength(2);
  await w.get('input[aria-label="显示名称"]').setValue("新轮询");
  await w.get('select[aria-label="支付方式"]').setValue("alipay");
  await w.get('select[aria-label="轮询方式"]').setValue("weighted");
  expect(w.text()).toContain("按权重随机轮询");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(sent).toEqual([
    {
      path: "/resources/pay-rolls/drafts",
      body: {
        creation_mode: "draft",
        name: "新轮询",
        type: "alipay",
        strategy: "weighted",
      },
    },
  ]);
  expect(w.emitted("saved")?.[0]?.[0]).toMatchObject({
    id: "42",
    enabled: false,
    default_route: false,
  });
});

it("locks an unknown draft creation and reads the list without dispatching another create", async () => {
  const c = client();
  let posts = 0;
  vi.spyOn(c, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST") {
      posts++;
      throw new TypeError("network interrupted");
    }
    if (path.includes("routing-options")) return catalog;
    return { items: [], total: 0 };
  });
  let w = mount(CreateForm, { props: { client: c } });
  mounted.push(w);
  await flushPromises();
  await w.get('input[aria-label="显示名称"]').setValue("待核对");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.text()).toContain("创建结果尚未确认");
  expect(button(w, "创建轮询组").attributes("disabled")).toBeDefined();
  await button(w, "读取列表核对").trigger("click");
  await flushPromises();
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(posts).toBe(1);
});

it("configures a same-type channel table with a CAS token and requires explicit confirmation for active groups", async () => {
  const c = client(),
    writes: unknown[] = [];
  vi.spyOn(c, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST") {
      writes.push({ path, body: init.body });
      return {
        ...record("b".repeat(64), true),
        fields: { ...record().fields, enabled: true, channel_ids: "7:2,8:3" },
      };
    }
    return path.includes("routing-options")
      ? catalog
      : record("a".repeat(64), true);
  });
  let w = mount(ChannelEditor, { props: { client: c, rollID: "41" } });
  mounted.push(w);
  await flushPromises();
  expect(w.find('[data-channel-id="9"]').exists()).toBe(false);
  await w.get('input[aria-label="选择通道 8"]').setValue(true);
  await w.get('input[aria-label="通道 8 权重"]').setValue("3");
  expect(button(w, "保存通道配置").attributes("disabled")).toBeDefined();
  await w.get('input[aria-label="确认修改已启用轮询组"]').setValue(true);
  await button(w, "保存通道配置").trigger("click");
  await flushPromises();
  expect(writes).toEqual([
    {
      path: "/resources/pay-rolls/41/channels",
      body: {
        expected_revision: "a".repeat(64),
        channel_ids: "7:2,8:3",
        confirm_enabled: true,
      },
    },
  ]);
  expect(w.emitted("saved")?.[0]?.[0]).toMatchObject({
    id: "41",
    fields: {
      merchant_id: "101",
      default_route: true,
      enabled: true,
      limits: '{"daily_count":7}',
    },
  });
});

it("clears editable channel drafts after unknown writes and requires explicit successful readback", async () => {
  const c = client();
  let posts = 0,
    failRead = false;
  vi.spyOn(c, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST") {
      posts++;
      failRead = true;
      throw new TypeError("network interrupted");
    }
    if (path.includes("routing-options")) return catalog;
    if (failRead) throw new ApiError("unavailable", 503, -1);
    return record();
  });
  let w = mount(ChannelEditor, { props: { client: c, rollID: "41" } });
  mounted.push(w);
  await flushPromises();
  await w.get('input[aria-label="选择通道 8"]').setValue(true);
  await button(w, "保存通道配置").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("保存结果尚未确认");
  w = await pageReload248(w);
  await flushPromises();
  expect(button(w, "保存通道配置").attributes("disabled")).toBeDefined();
  expect(w.text()).toContain("unavailable");
  failRead = false;
  w = await pageReload248(w);
  await flushPromises();
  expect(w.get('input[aria-label="选择通道 8"]').element).toHaveProperty(
    "checked",
    false,
  );
  expect(w.text()).not.toContain("保存结果尚未确认");
  expect(posts).toBe(1);
});

it("reads back 409 without retrying the write and preserves a repairable 422 draft", async () => {
  const c = client();
  let status = 422,
    posts = 0;
  vi.spyOn(c, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST") {
      posts++;
      throw new ApiError("rejected", status, -1);
    }
    return path.includes("routing-options") ? catalog : record();
  });
  let w = mount(ChannelEditor, { props: { client: c, rollID: "41" } });
  mounted.push(w);
  await flushPromises();
  await w.get('input[aria-label="选择通道 8"]').setValue(true);
  await button(w, "保存通道配置").trigger("click");
  await flushPromises();
  expect(w.get('input[aria-label="选择通道 8"]').element).toHaveProperty(
    "checked",
    true,
  );
  status = 409;
  await button(w, "保存通道配置").trigger("click");
  await flushPromises();
  expect(w.get('input[aria-label="选择通道 8"]').element).toHaveProperty(
    "checked",
    false,
  );
  expect(posts).toBe(2);
});

it("keeps the stored configuration readable when the runtime catalog fails and refuses channel writes", async () => {
  const c = client();
  vi.spyOn(c, "request").mockImplementation(async (path) => {
    if (path.includes("routing-options"))
      throw new ApiError("目录不可用", 503, -1);
    return record();
  });
  let w = mount(ChannelEditor, { props: { client: c, rollID: "41" } });
  mounted.push(w);
  await flushPromises();
  expect(w.text()).toContain("轮询甲");
  expect(w.text()).toContain("当前通道：7:2");
  expect(w.text()).toContain("目录不可用");
  expect(button(w, "保存通道配置").attributes("disabled")).toBeDefined();
});

it("does not let a late save response or finally block pollute another client/CSRF context", async () => {
  const c = client();
  let resolveOld!: (value: unknown) => void,
    resolveNew!: (value: unknown) => void;
  vi.spyOn(c, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST")
      return new Promise((resolve) => {
        resolveOld = resolve;
      });
    return path.includes("routing-options") ? catalog : record();
  });
  let w = mount(ChannelEditor, { props: { client: c, rollID: "41" } });
  mounted.push(w);
  await flushPromises();
  await w.get('input[aria-label="选择通道 8"]').setValue(true);
  await button(w, "保存通道配置").trigger("click");
  await flushPromises();
  const next = client();
  next.csrfToken = "new-csrf";
  vi.spyOn(next, "request").mockImplementation(async (path, init) => {
    if (init?.method === "POST")
      return new Promise((resolve) => {
        resolveNew = resolve;
      });
    return path.includes("routing-options") ? catalog : record("b".repeat(64));
  });
  await w.setProps({ client: next });
  await flushPromises();
  await w.get('input[aria-label="选择通道 8"]').setValue(true);
  await button(w, "保存通道配置").trigger("click");
  await flushPromises();
  resolveOld({
    ...record("c".repeat(64)),
    fields: { ...record().fields, channel_ids: "7:2,8:1" },
  });
  await flushPromises();
  expect(button(w, "正在保存…").attributes("disabled")).toBeDefined();
  expect(w.emitted("saved")).toBeUndefined();
  resolveNew({
    ...record("d".repeat(64)),
    fields: { ...record().fields, channel_ids: "7:2,8:1" },
  });
  await flushPromises();
  expect(w.emitted("saved")).toHaveLength(1);
});

it("treats an uncorrelated successful create response as unknown instead of claiming another draft", async () => {
  const c = client();
  vi.spyOn(c, "request").mockImplementation(async (_path, init) =>
    init?.method === "POST"
      ? {
          ...record().fields,
          id: "42",
          name: "another record",
          merchant_id: "0",
          enabled: false,
          default_route: false,
          channel_ids: "",
          limits: "{}",
          rules: "{}",
        }
      : catalog,
  );
  let w = mount(CreateForm, { props: { client: c } });
  mounted.push(w);
  await flushPromises();
  await w.get('input[aria-label="显示名称"]').setValue("expected name");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.emitted("saved")).toBeUndefined();
  expect(w.text()).toContain("创建结果尚未确认");
  expect(button(w, "创建轮询组").attributes("disabled")).toBeDefined();
});

it("edits channels on an existing supported record with omitted optional limits and rules", async () => {
  const c = client();
  const legacy = { ...record().fields } as Record<string, unknown>;
  delete legacy.limits;
  delete legacy.rules;
  vi.spyOn(c, "request").mockImplementation(async (path) =>
    path.includes("routing-options")
      ? catalog
      : { id: "41", revision: "a".repeat(64), fields: legacy },
  );
  let w = mount(ChannelEditor, { props: { client: c, rollID: "41" } });
  mounted.push(w);
  await flushPromises();
  expect(w.text()).toContain("轮询甲");
  await w.get('input[aria-label="选择通道 8"]').setValue(true);
  expect(button(w, "保存通道配置").attributes("disabled")).toBeUndefined();
});
