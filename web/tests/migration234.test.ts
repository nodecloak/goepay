import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Wizard from "../src/components/MigrationWizard234.vue";
import {
  parseMigrationCapabilities234,
  parseMigrationView234,
  prepareMigration234,
} from "../src/migration234";
import {
  flattenMenu,
  navigationFor,
  pageAuthorizedFor,
  viewKind,
} from "../src/menu";
import {
  migrationCapabilities234,
  migrationReport234,
  migrationView234,
} from "./migration234-fixtures";

const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
const response = (data: unknown, status = 200) =>
  new Response(
    JSON.stringify({
      code: status === 200 ? 0 : -1,
      message: "sanitized",
      data,
    }),
    { status },
  );
function setup(
  options: {
    target?: boolean;
    apply?: () => Promise<Response>;
    prepare?: () => Promise<Response>;
    props?: Record<string, unknown>;
  } = {},
) {
  const calls: { path: string; method: string; body: unknown }[] = [];
  const fetch = vi.fn(
    async (input: string | URL | Request, init?: RequestInit) => {
      const path = String(input),
        method = init?.method || "GET";
      calls.push({ path, method, body: init?.body });
      if (method === "GET" && path.endsWith("/epay"))
        return response(
          migrationCapabilities234({ target_ready: options.target ?? true }),
        );
      if (method === "POST" && path.endsWith("/prepare"))
        return options.prepare
          ? options.prepare()
          : response(
              migrationView234({ target_ready: options.target ?? true }),
            );
      if (method === "POST" && path.endsWith("/apply"))
        return options.apply
          ? options.apply()
          : response(
              migrationView234({
                status: "completed",
                progress: 100,
                report: migrationReport234(),
              }),
            );
      if (method === "GET" && path.endsWith("a".repeat(32)))
        return response(migrationView234());
      throw Error("unexpected fixture request");
    },
  );
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic234";
  const wrapper = mount(Wizard, {
    props: { client, ownSession: true, adminClass: "super", ...options.props },
    global: { stubs: { teleport: true } },
  });
  wrappers.push(wrapper);
  return { wrapper, client, calls, fetch };
}
function button(w: VueWrapper, text: string) {
  const b = w.findAll("button").find((x) => x.text() === text);
  if (!b) throw Error("missing button " + text);
  return b;
}
async function upload(w: VueWrapper) {
  const input = w.find('input[type="file"]');
  Object.defineProperty(input.element, "files", {
    value: [
      new File(["synthetic local dump"], "private-source234.sql", {
        type: "text/plain",
      }),
    ],
    configurable: true,
  });
  await input.trigger("change");
  await button(w, "上传并预检查").trigger("click");
  await flushPromises();
}
async function acknowledge(w: VueWrapper) {
  await w.find('[aria-label="确认源站停止写入且备份一致"]').setValue(true);
  await w.find('[aria-label="确认仅归档数据不会自动启用"]').setValue(true);
  await w.find('[aria-label="输入MIGRATE确认导入"]').setValue("MIGRATE");
}
describe("migration234 response and upload boundaries", () => {
  it("owns a super-only route", () => {
    expect(viewKind("admin", "/system/migration")).toBe("migration");
    expect(
      pageAuthorizedFor(
        "admin",
        "/system/migration",
        "admin",
        "super",
        undefined,
      ),
    ).toBe(true);
    expect(
      pageAuthorizedFor(
        "admin",
        "/system/migration",
        "admin",
        "operator",
        undefined,
      ),
    ).toBe(false);
    expect(
      flattenMenu(navigationFor("admin", "admin", "operator", undefined)).some(
        (x) => x.path === "/system/migration",
      ),
    ).toBe(false);
  });
  it("parses bounded, redacted metadata and accepts an optional expiry", () => {
    expect(
      parseMigrationView234(
        migrationView234({ expires_at: "2026-11-03T01:02:03Z" }),
      ).merchant_count,
    ).toBe(1);
    expect(
      parseMigrationCapabilities234(
        migrationCapabilities234({ runs: [migrationView234()] }),
      ).runs,
    ).toHaveLength(1);
  });
  it("can display blocked detection failures without inventing a prefix", () => {
    expect(
      parseMigrationView234(
        migrationView234({
          status: "blocked",
          prefix: "",
          issues: [{ path: "$", code: "missing_tables" }],
        }),
      ).prefix,
    ).toBe("");
  });
  it("accepts a proven unprefixed source family and does not guess it from prefixed tables", () => {
    const table_counts = [
      { name: "user", rows: 1, mode: "native" },
      { name: "order", rows: 2, mode: "native" },
      { name: "type", rows: 1, mode: "archive" },
    ];
    expect(
      parseMigrationView234(migrationView234({ prefix: "", table_counts }))
        .prefix,
    ).toBe("");
    expect(() =>
      parseMigrationView234(migrationView234({ prefix: "" })),
    ).toThrow();
  });
  it("accepts a 59-character prefix but rejects a 60-character prefix", () => {
    const prefix = "p".repeat(59),
      table_counts = [
        { name: prefix + "user", rows: 1, mode: "native" },
        { name: prefix + "order", rows: 2, mode: "native" },
        { name: prefix + "type", rows: 1, mode: "archive" },
      ];
    expect(
      parseMigrationView234(migrationView234({ prefix, table_counts })).prefix,
    ).toBe(prefix);
    expect(() =>
      parseMigrationView234(
        migrationView234({ prefix: prefix + "p", table_counts }),
      ),
    ).toThrow();
  });
  it.each([
    { merchant_count: 9 },
    { available_total: "11.00" },
    { valid: false },
    { applied: false, already_imported: false },
  ])(
    "does not label a mismatched completion receipt as success %s",
    (extra) => {
      expect(() =>
        parseMigrationView234(
          migrationView234({
            status: "completed",
            progress: 100,
            report: { ...migrationReport234(), ...extra },
          }),
        ),
      ).toThrow();
    },
  );
  it("rejects an execution receipt attached to a prepared task", () => {
    expect(() =>
      parseMigrationView234(migrationView234({ report: migrationReport234() })),
    ).toThrow();
  });
  it.each([
    { sha256: "not-a-hash" },
    { status: "success" },
    { progress: 101 },
    { available_total: "1e9" },
    { table_counts: [{ name: "secret<svg>", rows: 1, mode: "archive" }] },
    { issues: [{ path: "raw secret person", code: "invalid" }] },
    { api_key: "secret" },
  ])("rejects unsafe or unrecognized public data %s", (extra) =>
    expect(() => parseMigrationView234(migrationView234(extra))).toThrow(),
  );
  it("does not send an oversized upload or an unauthorized one", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const client = new ApiClient("admin");
    client.csrfToken = "csrf";
    const file = new File(["x"], "backup.sql");
    Object.defineProperty(file, "size", { value: 67108865 });
    await expect(
      prepareMigration234(client, "epay_pro", "", file),
    ).rejects.toThrow();
    client.area = "merchant";
    await expect(
      prepareMigration234(
        client,
        "epay_pro",
        "",
        new File(["x"], "backup.sql"),
      ),
    ).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  });
});
describe("migration234 wizard interactions", () => {
  it.each(["prepare", "apply"] as const)(
    "explains HTTP429 capacity limits safely during %s without retrying",
    async (kind) => {
      const refuse = async () =>
        new Response(
          JSON.stringify({
            code: -1,
            message: "private server connection and secret",
            data: null,
          }),
          { status: 429 },
        );
      const f = setup(
        kind === "prepare" ? { prepare: refuse } : { apply: refuse },
      );
      await flushPromises();
      await upload(f.wrapper);
      if (kind === "apply") {
        await acknowledge(f.wrapper);
        await button(f.wrapper, "开始导入").trigger("click");
        await flushPromises();
      }
      expect(f.wrapper.get('[role="alert"]').text()).toBe(
        "迁移繁忙或暂存次数、空间达到上限，请先读取迁移记录；持续出现请联系管理员处理暂存。",
      );
      expect(f.wrapper.text()).not.toContain(
        "private server connection and secret",
      );
      expect(
        button(f.wrapper, "读取迁移记录").attributes("disabled"),
      ).toBeUndefined();
      await button(f.wrapper, "读取迁移记录").trigger("click");
      await flushPromises();
      expect(f.calls.filter((x) => x.path.endsWith("/" + kind))).toHaveLength(
        1,
      );
    },
  );
  it("lets the user correct an invalid local prefix without claiming an unknown upload", async () => {
    const f = setup();
    await flushPromises();
    await f.wrapper.find('[aria-label="源表前缀"]').setValue("bad-prefix;");
    const input = f.wrapper.find("input[type=file]");
    Object.defineProperty(input.element, "files", {
      value: [new File(["x"], "source.sql")],
      configurable: true,
    });
    await input.trigger("change");
    expect(
      button(f.wrapper, "上传并预检查").attributes("disabled"),
    ).toBeDefined();
    expect(f.wrapper.text()).toContain("表前缀仅允许字母、数字和下划线");
    expect(f.calls.filter((x) => x.path.endsWith("/prepare"))).toHaveLength(0);
    await f.wrapper.find('[aria-label="源表前缀"]').setValue("pre_");
    expect(
      button(f.wrapper, "上传并预检查").attributes("disabled"),
    ).toBeUndefined();
  });
  it("loads capabilities without mutations and restricts own super sessions", async () => {
    const f = setup();
    await flushPromises();
    expect(f.calls).toHaveLength(1);
    expect(f.calls[0].method).toBe("GET");
    expect(f.wrapper.text()).toContain("仅归档");
    await f.wrapper.setProps({ ownSession: false });
    await flushPromises();
    expect(f.wrapper.text()).toContain("本人超级管理员");
    expect(f.wrapper.find("input[type=file]").exists()).toBe(false);
  });
  it("shows native and archived data, then imports exactly once after explicit consent", async () => {
    const f = setup();
    await flushPromises();
    await upload(f.wrapper);
    expect(f.wrapper.text()).toContain("pre_config");
    expect(f.wrapper.text()).not.toContain("private-source234.sql");
    expect(button(f.wrapper, "开始导入").attributes("disabled")).toBeDefined();
    await acknowledge(f.wrapper);
    await button(f.wrapper, "开始导入").trigger("click");
    await flushPromises();
    expect(f.wrapper.text()).toContain("导入完成，待配置和启用");
    expect(f.calls.filter((x) => x.path.endsWith("/apply"))).toHaveLength(1);
    expect(
      JSON.parse(String(f.calls.find((x) => x.path.endsWith("/apply"))?.body)),
    ).toEqual({
      expected_sha256: "b".repeat(64),
      source_frozen: true,
      archive_acknowledged: true,
      confirmation: "MIGRATE",
    });
    expect(
      f.calls.find((x) => x.path.endsWith("/prepare"))?.body,
    ).toBeInstanceOf(FormData);
  });
  it("allows analysis on an existing installation while blocking import", async () => {
    const f = setup({ target: false });
    await flushPromises();
    await upload(f.wrapper);
    expect(f.wrapper.text()).toContain("已有业务数据");
    await acknowledge(f.wrapper);
    expect(button(f.wrapper, "开始导入").attributes("disabled")).toBeDefined();
    expect(f.calls.filter((x) => x.path.endsWith("/apply"))).toHaveLength(0);
  });
  it("does not repeat an apply whose response is unknown, even after a prepared status read", async () => {
    const f = setup({
      apply: async () => {
        throw Error("private source credential");
      },
    });
    await flushPromises();
    await upload(f.wrapper);
    await acknowledge(f.wrapper);
    await button(f.wrapper, "开始导入").trigger("click");
    await flushPromises();
    expect(f.wrapper.text()).toContain("结果尚未确认");
    expect(f.wrapper.text()).not.toContain("private source credential");
    await button(f.wrapper, "读取当前任务状态").trigger("click");
    await flushPromises();
    expect(f.calls.filter((x) => x.path.endsWith("/apply"))).toHaveLength(1);
    expect(button(f.wrapper, "开始导入").attributes("disabled")).toBeDefined();
  });
  it("ignores a second click while the first import is in flight", async () => {
    let resolve!: (v: Response) => void;
    const f = setup({
      apply: () =>
        new Promise((r) => {
          resolve = r;
        }),
    });
    await flushPromises();
    await upload(f.wrapper);
    await acknowledge(f.wrapper);
    await button(f.wrapper, "开始导入").trigger("click");
    await button(f.wrapper, "开始导入").trigger("click");
    expect(f.calls.filter((x) => x.path.endsWith("/apply"))).toHaveLength(1);
    resolve(
      response(
        migrationView234({
          status: "completed",
          progress: 100,
          report: migrationReport234(),
        }),
      ),
    );
    await flushPromises();
    expect(f.wrapper.text()).toContain("导入完成，待配置和启用");
  });
  it("does not automatically repeat a lost prepare response", async () => {
    const f = setup({
      prepare: async () => {
        throw Error("raw sql secret");
      },
    });
    await flushPromises();
    await upload(f.wrapper);
    expect(f.wrapper.text()).toContain("预检查结果尚未确认");
    await button(f.wrapper, "读取迁移记录").trigger("click");
    await flushPromises();
    expect(f.calls.filter((x) => x.path.endsWith("/prepare"))).toHaveLength(1);
    expect(f.wrapper.text()).not.toContain("raw sql secret");
    expect(
      button(f.wrapper, "上传并预检查").attributes("disabled"),
    ).toBeDefined();
  });
  it("clears private drafts and rejects a late result after session replacement", async () => {
    let resolve!: (v: Response) => void;
    const f = setup({
      prepare: () =>
        new Promise((r) => {
          resolve = r;
        }),
    });
    await flushPromises();
    const input = f.wrapper.find("input[type=file]");
    Object.defineProperty(input.element, "files", {
      value: [new File(["x"], "source.sql")],
      configurable: true,
    });
    await input.trigger("change");
    await button(f.wrapper, "上传并预检查").trigger("click");
    const other = new ApiClient("admin");
    other.csrfToken = "different";
    await f.wrapper.setProps({ client: other });
    await flushPromises();
    resolve(response(migrationView234()));
    await flushPromises();
    expect(f.wrapper.text()).not.toContain("pre_config");
    expect(f.calls.filter((x) => x.path.endsWith("/apply"))).toHaveLength(0);
  });
});
