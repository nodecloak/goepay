// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { ApiClient, ApiError } from "../src/api";
import Page from "../src/components/WeixinAppsPage.vue";

const sourceID = "9007199254740993",
  mid = "9007199254740997";
const managed = (changes: Record<string, unknown> = {}) => ({
  id: "7",
  revision: "2",
  name: "原公众号",
  kind: "official_account",
  app_id: "wxsynthetic",
  enabled: false,
  archived: false,
  source_channel_id: "0",
  merchant_ids: [],
  path: "",
  fingerprint: "a".repeat(64),
  secret_configured: true,
  draft: true,
  execution_ready: false,
  origin: "managed",
  read_only: false,
  ...changes,
});
const legacy = () =>
  managed({
    revision: "0",
    name: "旧资料",
    fingerprint: "",
    origin: "legacy_metadata",
    read_only: true,
    secret_configured: false,
  });
const imported = () =>
  managed({
    id: "8",
    revision: "1",
    name: "进程导入资料",
    fingerprint: "",
    origin: "imported",
    read_only: true,
  });
const caps = (write = true) => ({
  can_write: write,
  sources: write
    ? [{ id: sourceID, fingerprint: "f".repeat(64), merchant_ids: ["1", mid] }]
    : [],
});
const mounted: VueWrapper[] = [];
afterEach(() => {
  mounted.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
});
const button = (w: VueWrapper, label: string) =>
  w.findAll("button").find((n) => n.text() === label)!;
async function fixture(
  options: {
    write?: boolean;
    capabilities?: unknown;
    configurationErrors?: unknown;
    items?: unknown[];
    post?: (path: string, init: Record<string, unknown>) => Promise<unknown>;
    receipt?: () => Promise<unknown>;
  } = {},
) {
  const client = reactive(new ApiClient("admin"));
  client.csrfToken = "synthetic-weixin-csrf";
  const requests: { path: string; method?: string; body?: unknown }[] = [];
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, init) => {
      requests.push({ path, method: init?.method, body: init?.body });
      if (init?.method && init.method !== "GET")
        return options.post
          ? options.post(path, init as Record<string, unknown>)
          : managed({ revision: "3", fingerprint: "b".repeat(64) });
      if (path.endsWith("/capabilities"))
        return options.capabilities === undefined
          ? caps(options.write !== false)
          : options.capabilities;
      if (path.includes("/requests/"))
        return options.receipt
          ? options.receipt()
          : managed({ revision: "3", fingerprint: "b".repeat(64) });
      return {
        items: options.items || [managed(), legacy(), imported()],
        total: options.items?.length ?? 3,
        can_write: options.write !== false,
        ...(options.configurationErrors === undefined
          ? {}
          : { configuration_errors: options.configurationErrors }),
      };
    });
  const w = mount(Page, { props: { client }, attachTo: document.body });
  mounted.push(w);
  await flushPromises();
  return { w, client, request, requests };
}
const row = (w: VueWrapper, origin = "managed", id = "7") =>
  w.get(`[data-app-origin="${origin}"][data-app-id="${id}"]`);
async function openEdit(w: VueWrapper) {
  await row(w)
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
}
async function fillCreate(w: VueWrapper) {
  await button(w, "新增应用").trigger("click");
  await flushPromises();
  await w.get('input[aria-label="名称"]').setValue("新应用");
  await w.get('input[aria-label="APPID"]').setValue("wxnewsynthetic");
  await w
    .get('input[aria-label="APPSECRET"]')
    .setValue("synthetic-private-appsecret");
}

it("keeps colliding legacy/managed IDs separate and enforces actual read-only capability", async () => {
  const { w, requests } = await fixture({ write: false });
  expect(w.findAll("tbody tr")).toHaveLength(3);
  expect(row(w).text()).toContain("原公众号");
  expect(row(w, "legacy_metadata").text()).toContain("旧资料");
  expect(button(w, "新增应用")).toBeUndefined();
  expect(row(w).text()).not.toContain("编辑");
  expect(row(w, "legacy_metadata").text()).not.toContain("配置为新应用");
  expect(w.text()).toContain("只读");
  expect(requests.some((r) => r.method && r.method !== "GET")).toBe(false);
});

it("creates an inert encrypted application draft with four basic fields and a fresh 64-hex request", async () => {
  const { w, requests } = await fixture({
    post: async (_path, init) => {
      const body = init.body as Record<string, unknown>;
      return managed({
        id: "9",
        revision: "1",
        name: body.name,
        kind: body.kind,
        app_id: body.app_id,
        fingerprint: "c".repeat(64),
      });
    },
  });
  await fillCreate(w);
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  const write = requests.find((r) => r.method === "POST")!;
  expect(write.path).toBe("/weixin-applications");
  expect(write.body).toMatchObject({
    kind: "official_account",
    name: "新应用",
    app_id: "wxnewsynthetic",
    appsecret: "synthetic-private-appsecret",
    enabled: false,
    source_channel_id: "0",
    merchant_ids: [],
    path: "",
    expected_source_fingerprint: "",
  });
  expect((write.body as Record<string, unknown>).request_id).toMatch(
    /^[a-f0-9]{64}$/,
  );
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.html()).not.toContain("synthetic-private-appsecret");
});

it("keeps an existing secret blank, sends CAS and exact source/merchant strings, and requires new secret for a changed APPID", async () => {
  const { w, requests } = await fixture({
    post: async (_path, init) => {
      const body = init.body as Record<string, unknown>;
      return managed({
        name: body.name,
        kind: body.kind,
        app_id: body.app_id,
        enabled: body.enabled,
        source_channel_id: body.source_channel_id,
        merchant_ids: body.merchant_ids,
        path: body.path,
        revision: "3",
        fingerprint: "b".repeat(64),
        secret_configured: true,
        draft: false,
        execution_ready: false,
      });
    },
  });
  await openEdit(w);
  expect(w.get('input[aria-label="APPSECRET"]').element).toHaveProperty(
    "value",
    "",
  );
  await w.get('select[aria-label="关联来源通道"]').setValue(sourceID);
  await w.get(`input[aria-label="允许商户 ${mid}"]`).setValue(true);
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  const write = requests.find((r) => r.method === "PATCH")!;
  expect(write.path).toBe("/weixin-applications/7");
  expect(write.body).toMatchObject({
    expected_revision: "2",
    expected_fingerprint: "a".repeat(64),
    appsecret: "",
    source_channel_id: sourceID,
    merchant_ids: [mid],
    expected_source_fingerprint: "f".repeat(64),
  });
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  await openEdit(w);
  await w.get('input[aria-label="APPID"]').setValue("wxchanged");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
  expect(w.get('[role="alert"]').text()).toContain(
    "更改类别或APPID时必须填写新的APPSECRET",
  );
});

it("uses legacy metadata only to prefill a new POST and never patches its colliding managed ID", async () => {
  const { w, requests } = await fixture();
  await row(w, "legacy_metadata")
    .findAll("button")
    .find((b) => b.text() === "配置为新应用")!
    .trigger("click");
  await flushPromises();
  expect(w.get('input[aria-label="APPID"]').element).toHaveProperty(
    "value",
    "wxsynthetic",
  );
  expect(w.get('input[aria-label="APPSECRET"]').element).toHaveProperty(
    "value",
    "",
  );
  expect(w.get('select[aria-label="关联来源通道"]').element).toHaveProperty(
    "value",
    "0",
  );
  expect(w.text()).toContain("新建");
  expect(requests.some((r) => r.method === "PATCH")).toBe(false);
});

it("clears the actual secret and blocks every write after unknown outcome; 404 receipt keeps it locked", async () => {
  let found = false;
  const { w, requests } = await fixture({
    post: async () => {
      throw new TypeError("connection lost");
    },
    receipt: async () => {
      if (!found) throw new ApiError("not found", 404, -1);
      return managed({
        id: "9",
        revision: "1",
        name: "新应用",
        app_id: "wxnewsynthetic",
        fingerprint: "c".repeat(64),
      });
    },
  });
  await fillCreate(w);
  const secret = w.get<HTMLInputElement>(
    'input[aria-label="APPSECRET"]',
  ).element;
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(secret.value).toBe("");
  expect(w.text()).toContain("结果尚未确认");
  await w.get('[role="dialog"] form').trigger("submit");
  await button(w, "查询原请求").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("结果尚未确认");
  expect(requests.filter((r) => r.method === "POST")).toHaveLength(1);
  const post = requests.find((r) => r.method === "POST")!.body as Record<
    string,
    unknown
  >;
  expect(
    requests.some(
      (r) => r.path === `/weixin-applications/requests/${post.request_id}`,
    ),
  ).toBe(true);
  found = true;
  await button(w, "查询原请求").trigger("click");
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(requests.filter((r) => r.method === "POST")).toHaveLength(1);
});

it("reads back a CAS conflict without retry and keeps 422 fields repairable", async () => {
  let status = 422;
  const { w, requests } = await fixture({
    post: async () => {
      throw new ApiError("rejected", status, -1);
    },
  });
  await openEdit(w);
  await w.get('input[aria-label="名称"]').setValue("可修复名称");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(w.get('input[aria-label="名称"]').element).toHaveProperty(
    "value",
    "可修复名称",
  );
  status = 409;
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(2);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).toContain("版本已变化");
});

it("requires explicit archive confirmation and sends only request/CAS fields", async () => {
  const { w, requests } = await fixture({
    post: async () =>
      managed({
        revision: "3",
        archived: true,
        enabled: false,
        fingerprint: "b".repeat(64),
      }),
  });
  await row(w)
    .findAll("button")
    .find((b) => b.text() === "归档")!
    .trigger("click");
  await flushPromises();
  expect(requests.some((r) => r.method === "POST")).toBe(false);
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  const write = requests.find((r) => r.method === "POST")!;
  expect(write.path).toBe("/weixin-applications/7/archive");
  expect(Object.keys(write.body as object).sort()).toEqual([
    "expected_fingerprint",
    "expected_revision",
    "request_id",
  ]);
});

it("erases secret data on context replacement and ignores a late mutation response", async () => {
  let finish!: (v: unknown) => void;
  const { w, requests } = await fixture({
    post: async () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  });
  await fillCreate(w);
  const secret = w.get<HTMLInputElement>(
    'input[aria-label="APPSECRET"]',
  ).element;
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  const next = reactive(new ApiClient("admin"));
  next.csrfToken = "new-context";
  vi.spyOn(next, "request").mockImplementation(async (path) =>
    path.endsWith("/capabilities")
      ? caps(false)
      : { items: [], total: 0, can_write: false },
  );
  await w.setProps({ client: next });
  await flushPromises();
  expect(secret.value).toBe("");
  finish(
    managed({
      id: "9",
      revision: "1",
      name: "新应用",
      app_id: "wxnewsynthetic",
    }),
  );
  await flushPromises();
  expect(w.text()).not.toContain("新应用");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(requests.filter((r) => r.method === "POST")).toHaveLength(1);
});

it("does not confirm a decoded success with a different source or permitted merchant binding", async () => {
  const { w, requests } = await fixture({
    post: async () =>
      managed({
        revision: "3",
        fingerprint: "b".repeat(64),
        source_channel_id: sourceID,
        merchant_ids: ["1"],
        draft: false,
      }),
  });
  await openEdit(w);
  await w.get('select[aria-label="关联来源通道"]').setValue(sourceID);
  await w.get(`input[aria-label="允许商户 ${mid}"]`).setValue(true);
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  expect(w.text()).toContain("结果尚未确认");
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
});

it("can save a disabled encrypted draft when the real writable capability has no executable sources", async () => {
  const { w, requests } = await fixture({
    capabilities: { can_write: true, sources: [] },
    post: async (_path, init) => {
      const body = init.body as Record<string, unknown>;
      return managed({
        id: "9",
        revision: "1",
        name: body.name,
        app_id: body.app_id,
        fingerprint: "c".repeat(64),
      });
    },
  });
  await fillCreate(w);
  expect(w.text()).toContain("当前没有可关联的来源通道");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(requests.find((r) => r.method === "POST")?.body).toMatchObject({
    enabled: false,
    source_channel_id: "0",
    merchant_ids: [],
    expected_source_fingerprint: "",
  });
  expect(w.find('[role="dialog"]').exists()).toBe(false);
});

it("uses both returned write flags rather than the admin area or a single optimistic list flag", async () => {
  const { w, requests } = await fixture({ capabilities: caps(false) });
  expect(row(w).text()).toContain("原公众号");
  expect(button(w, "新增应用")).toBeUndefined();
  expect(row(w).text()).not.toContain("编辑");
  expect(requests.some((r) => r.method && r.method !== "GET")).toBe(false);
});

it("keeps the reference ID/category/name/APPID/actions columns first and labels non-executable metadata clearly", async () => {
  const { w } = await fixture();
  expect(
    w
      .findAll("thead th")
      .slice(0, 5)
      .map((cell) => cell.text()),
  ).toEqual(["ID", "类别", "名称", "APPID", "操作"]);
  expect(row(w).findAll("td")[0]!.text()).toBe("7");
  expect(row(w, "legacy_metadata").text()).toContain("未接入资料 · 只读");
  expect(row(w, "imported", "8").text()).toContain("配置文件导入 · 只读");
});

it("shows corrupt managed configuration IDs separately while keeping healthy and same-ID imported records usable", async () => {
  const { w } = await fixture({
    configurationErrors: [{ id: "8", message: "应用配置损坏，请检查原记录" }],
  });
  expect(w.get('[role="alert"]').text()).toContain("应用 #8");
  expect(w.get('[role="alert"]').text()).toContain(
    "应用配置损坏，请检查原记录",
  );
  expect(w.findAll("tbody tr")).toHaveLength(3);
  expect(row(w, "imported", "8").text()).toContain("进程导入资料");
  expect(button(w, "新增应用").attributes("disabled")).toBeUndefined();
  await openEdit(w);
  expect(w.find('[role="dialog"]').exists()).toBe(true);
});

it("applies name/APPID/ID and category filters locally, resets both, and retains configuration alerts", async () => {
  const { w, requests } = await fixture({
    write: false,
    configurationErrors: [{ id: "9", message: "应用配置损坏，请检查原记录" }],
  });
  await w.get('input[aria-label="搜索应用"]').setValue("旧资料");
  expect(w.findAll("tbody tr")).toHaveLength(3);
  await button(w, "查询").trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")).toHaveLength(1);
  expect(row(w, "legacy_metadata").attributes("data-app-id")).toBe("7");
  await w.get('select[aria-label="筛选类别"]').setValue("mini_program");
  await button(w, "查询").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("没有符合条件的应用");
  expect(w.get('[role="alert"]').text()).toContain("应用 #9");
  await button(w, "重置").trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")).toHaveLength(3);
  expect(w.get('input[aria-label="搜索应用"]').element).toHaveProperty(
    "value",
    "",
  );
  expect(w.get('select[aria-label="筛选类别"]').element).toHaveProperty(
    "value",
    "",
  );
  await w.get('input[aria-label="搜索应用"]').setValue("8");
  await button(w, "查询").trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")).toHaveLength(1);
  expect(row(w, "imported", "8").attributes("data-app-id")).toBe("8");
  await w.get('input[aria-label="搜索应用"]').setValue("WXSYNTHETIC");
  await button(w, "查询").trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")).toHaveLength(3);
  expect(requests).toHaveLength(2);
});

it("paginates the complete healthy list locally and returns to the first page when applying or resetting filters", async () => {
  const records = Array.from({ length: 45 }, (_, index) =>
    managed({
      id: String(index + 1),
      name: `应用 ${index + 1}`,
      app_id: `wx${index + 1}`,
    }),
  );
  const { w, requests } = await fixture({ write: false, items: records });
  expect(w.findAll("tbody tr")).toHaveLength(20);
  expect(w.text()).toContain("共 45 条");
  await w.get('button[aria-label="下一页"]').trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")[0]!.findAll("td")[0]!.text()).toBe("21");
  await w.get('input[aria-label="搜索应用"]').setValue("应用 45");
  await button(w, "查询").trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")).toHaveLength(1);
  expect(row(w, "managed", "45").attributes("data-app-id")).toBe("45");
  await button(w, "重置").trigger("click");
  await flushPromises();
  expect(w.findAll("tbody tr")).toHaveLength(20);
  expect(w.findAll("tbody tr")[0]!.findAll("td")[0]!.text()).toBe("1");
  expect(requests).toHaveLength(2);
});

it("clears a rejected replacement secret and blocks a corrected name from silently keeping the old credential", async () => {
  let attempts = 0;
  const { w, requests } = await fixture({
    post: async (_path, init) => {
      attempts++;
      if (attempts === 1) throw new ApiError("资料无效", 422, -1);
      const body = init.body as Record<string, unknown>;
      return managed({
        name: body.name,
        revision: "3",
        fingerprint: "b".repeat(64),
      });
    },
  });
  await openEdit(w);
  const input = w.get<HTMLInputElement>(
    'input[aria-label="APPSECRET"]',
  ).element;
  await w
    .get('input[aria-label="APPSECRET"]')
    .setValue("synthetic-new-rotation-secret");
  await w.get('input[aria-label="名称"]').setValue("Rejected Name");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(input.value).toBe("");
  expect(w.text()).toContain("本次凭证尚未保存");
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
  await w.get('input[aria-label="名称"]').setValue("Corrected Name");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(1);
  expect(w.get('[role="alert"]').text()).toContain("重新输入APPSECRET");
  await w
    .get('input[aria-label="APPSECRET"]')
    .setValue("synthetic-new-rotation-secret");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(2);
  expect(requests.filter((r) => r.method === "PATCH")[1]!.body).toMatchObject({
    name: "Corrected Name",
    appsecret: "synthetic-new-rotation-secret",
  });
  expect(input.value).toBe("");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
});

it("keeps old credentials after a rejected rotation only when the user explicitly abandons the replacement", async () => {
  let attempts = 0;
  const { w, requests } = await fixture({
    post: async (_path, init) => {
      attempts++;
      if (attempts === 1) throw new ApiError("资料无效", 422, -1);
      return managed({
        name: (init.body as Record<string, unknown>).name,
        revision: "3",
        fingerprint: "b".repeat(64),
      });
    },
  });
  await openEdit(w);
  await w
    .get('input[aria-label="APPSECRET"]')
    .setValue("synthetic-abandoned-secret");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(w.get('input[aria-label="APPSECRET"]').element).toHaveProperty(
    "value",
    "",
  );
  await button(w, "放弃本次凭证轮换").trigger("click");
  await w.get('input[aria-label="名称"]').setValue("Keep Old Explicitly");
  await w.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(2);
  expect(requests.filter((r) => r.method === "PATCH")[1]!.body).toMatchObject({
    name: "Keep Old Explicitly",
    appsecret: "",
  });
  expect(w.find('[role="dialog"]').exists()).toBe(false);
});
