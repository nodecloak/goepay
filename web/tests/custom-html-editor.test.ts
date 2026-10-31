// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import CustomHtmlEditor from "../src/components/CustomHtmlEditor.vue";

const head0 = {
  version: 0,
  revision: "0",
  html: "",
  css: "",
  frame: true,
  created_at: "",
  actor: "",
  restored_from: 0,
};
const emptyVersions = { items: [] as unknown[] };

function client(request: ReturnType<typeof vi.fn>) {
  return Object.assign(new ApiClient("admin"), { request, csrfToken: "c" });
}

const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  vi.restoreAllMocks();
});

function mountEditor(request: ReturnType<typeof vi.fn>) {
  const w = mount(CustomHtmlEditor, { props: { client: client(request) } });
  mounted.push(w);
  return w;
}

it("reads the head and versions on mount and shows byte counters", async () => {
  const request = vi.fn(async (path: string) => {
    if (path.endsWith("/versions")) return emptyVersions;
    return head0;
  });
  const w = mountEditor(request);
  await flushPromises();
  expect(request.mock.calls.map((c) => c[0])).toEqual([
    "/templates/home/custom/html",
    "/templates/home/custom/versions",
  ]);
  // Empty editors read 0.0 KiB.
  expect(w.findAll(".custom-counter").map((c) => c.text())).toEqual([
    "0.0 KiB / 64 KiB",
    "0.0 KiB / 32 KiB",
  ]);
  // A multi-byte string counts UTF-8 bytes, not code points.
  await w.get("#custom-html-input").setValue("中"); // 3 bytes
  expect(w.get(".custom-counter").text()).toBe("0.0 KiB / 64 KiB");
  await w.get("#custom-html-input").setValue("a".repeat(2048));
  expect(w.get(".custom-counter").text()).toBe("2.0 KiB / 64 KiB");
});

it("rejects HTML over 64 KiB before any request and marks the counter over", async () => {
  const request = vi.fn(async (path: string) =>
    path.endsWith("/versions") ? emptyVersions : head0,
  );
  const w = mountEditor(request);
  await flushPromises();
  request.mockClear();
  await w.get("#custom-html-input").setValue("a".repeat(65537));
  expect(w.get(".custom-counter").classes()).toContain("is-over");
  await w.get("form.custom-html-form").trigger("submit");
  await flushPromises();
  expect(w.find(".template-confirm").exists()).toBe(false);
  expect(w.get("[role=alert]").text()).toBe("HTML 超过 64 KiB");
  expect(request).not.toHaveBeenCalled();
});

it("saves a new version with CAS revision and shows the removed report", async () => {
  const puts: { path: string; body: any }[] = [];
  let versions: any[] = [];
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/versions")) return { items: versions };
    if (options?.method === "PUT") {
      puts.push({ path, body: options.body });
      versions = [
        {
          version: 4,
          created_at: "2026-10-31T10:20:05Z",
          actor: "admin",
          html_bytes: 11,
          css_bytes: 0,
          frame: true,
          restored_from: 0,
          notice: "pending",
        },
      ];
      return {
        version: 4,
        revision: "13",
        removed: { "<script>": 1, onclick: 2 },
      };
    }
    // Head after save reports version 4.
    return { ...head0, version: 4, revision: "13", html: "<p>hello</p>" };
  });
  // First head load returns version 3.
  request.mockImplementationOnce(async () => ({
    ...head0,
    version: 3,
    revision: "12",
    html: "<p>hi</p>",
  }));
  const w = mountEditor(request);
  await flushPromises();
  await w.get("#custom-html-input").setValue("<p>hello</p>");
  await w.get("form.custom-html-form").trigger("submit");
  // Two-step confirm names the next version.
  const confirm = w.get(".template-confirm");
  expect(confirm.text()).toContain("保存后会生成第 4 版");
  await confirm.get("button.primary").trigger("click");
  await flushPromises();
  expect(puts).toHaveLength(1);
  expect(puts[0].body).toEqual({
    expected_revision: "12",
    html: "<p>hello</p>",
    css: "",
    frame: true,
  });
  expect(w.get("[role=status].success").text()).toContain("已保存第 4 版");
  expect(w.get("[role=status].success").text()).toContain(
    "已移除：<script> ×1，onclick ×2",
  );
  // The version table shows the new row with its notice state.
  expect(w.get("tbody").text()).toContain("第 4 版");
  expect(w.get("tbody").text()).toContain("等待发送");
});

it("reports no removals when the server removed nothing", async () => {
  let versions: any[] = [];
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/versions")) return { items: versions };
    if (options?.method === "PUT") {
      versions = [
        {
          version: 1,
          created_at: "2026-10-31T10:20:05Z",
          actor: "admin",
          html_bytes: 5,
          css_bytes: 0,
          frame: false,
          restored_from: 0,
          notice: "accepted",
        },
      ];
      return { version: 1, revision: "2", removed: {} };
    }
    return { ...head0, version: 1, revision: "2" };
  });
  const w = mountEditor(request);
  await flushPromises();
  await w.get("#custom-html-input").setValue("<p>x</p>");
  await w.get("form.custom-html-form").trigger("submit");
  await w.get(".template-confirm button.primary").trigger("click");
  await flushPromises();
  expect(w.get("[role=status].success").text()).toContain("未移除任何内容");
  expect(w.get("tbody").text()).toContain("已提交");
});

it("blocks repeated saves on a 409 and asks to reload instead of auto-retrying", async () => {
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/versions")) return emptyVersions;
    if (options?.method === "PUT") throw new ApiError("版本冲突", 409, 0);
    return { ...head0, version: 1, revision: "5", html: "<p>a</p>" };
  });
  const w = mountEditor(request);
  await flushPromises();
  await w.get("#custom-html-input").setValue("<p>b</p>");
  await w.get("form.custom-html-form").trigger("submit");
  await w.get(".template-confirm button.primary").trigger("click");
  await flushPromises();
  expect(w.get("[role=alert]").text()).toContain("版本已变化");
  expect(w.get("[role=alert]").text()).toContain("禁止自动重发");
  // The form is now locked (uncertain) until reload.
  expect(w.get("form.custom-html-form fieldset").attributes("disabled")).toBe(
    "",
  );
});

it("shows the server message verbatim on a 422", async () => {
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/versions")) return emptyVersions;
    if (options?.method === "PUT")
      throw new ApiError("CSS 不能使用 url()", 422, 0);
    return { ...head0, version: 1, revision: "7", html: "<p>a</p>" };
  });
  const w = mountEditor(request);
  await flushPromises();
  await w.get("#custom-css-input").setValue("a{background:url(x)}");
  await w.get("form.custom-html-form").trigger("submit");
  await w.get(".template-confirm button.primary").trigger("click");
  await flushPromises();
  expect(w.get("[role=alert]").text()).toBe("CSS 不能使用 url()");
  // A plain invalid response is not treated as a lock.
  expect(
    w.get("form.custom-html-form fieldset").attributes("disabled"),
  ).toBeUndefined();
});

it("confirms before loading the sample over a non-empty editor and posts the body on restore", async () => {
  let versions: any[] = [
    {
      version: 2,
      created_at: "2026-10-31T10:20:05Z",
      actor: "admin",
      html_bytes: 9,
      css_bytes: 3,
      frame: true,
      restored_from: 0,
      notice: "accepted",
    },
  ];
  const posts: { path: string; body: any }[] = [];
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/versions")) return { items: versions };
    if (path.endsWith("/sample"))
      return {
        html: "<section>示例</section>",
        css: ".a{color:red}",
        frame: false,
      };
    if (path.includes("/restore")) {
      posts.push({ path, body: options.body });
      versions = [
        {
          version: 3,
          created_at: "2026-10-31T10:30:05Z",
          actor: "admin",
          html_bytes: 9,
          css_bytes: 3,
          frame: true,
          restored_from: 2,
          notice: "pending",
        },
        ...versions,
      ];
      return { version: 3, revision: "9", removed: {} };
    }
    return { ...head0, version: 2, revision: "8", html: "<p>keep</p>" };
  });
  const w = mountEditor(request);
  await flushPromises();
  // 载入示例 over a non-empty editor asks first.
  await w
    .findAll("button")
    .find((b) => b.text() === "载入示例")!
    .trigger("click");
  expect(w.get(".template-confirm").text()).toContain("载入示例会覆盖");
  await w.get(".template-confirm button.primary").trigger("click");
  await flushPromises();
  expect(
    (w.get("#custom-html-input").element as HTMLTextAreaElement).value,
  ).toBe("<section>示例</section>");
  // The version table shows 来源 for a normal save (第 2 版).
  expect(w.get("tbody").text()).toContain("保存");
  // 恢复 confirms, then posts the expected revision.
  await w
    .findAll("tbody button")
    .find((b) => b.text() === "恢复")!
    .trigger("click");
  await w.get(".template-confirm button.primary").trigger("click");
  await flushPromises();
  expect(posts).toHaveLength(1);
  expect(posts[0].path).toBe("/templates/home/custom/versions/2/restore");
  expect(posts[0].body).toEqual({ expected_revision: "8" });
  expect(w.get("[role=status].success").text()).toContain("已恢复为第 3 版");
  expect(w.get("tbody").text()).toContain("恢复自第 2 版");
});

it("emits a version preview with the sandboxed per-version URL", async () => {
  const request = vi.fn(async (path: string) => {
    if (path.endsWith("/versions"))
      return {
        items: [
          {
            version: 5,
            created_at: "2026-10-31T10:20:05Z",
            actor: "admin",
            html_bytes: 9,
            css_bytes: 3,
            frame: true,
            restored_from: 0,
            notice: "unknown",
          },
        ],
      };
    return { ...head0, version: 5, revision: "20" };
  });
  const w = mountEditor(request);
  await flushPromises();
  expect(w.get("tbody").text()).toContain("结果未知");
  await w
    .findAll("tbody button")
    .find((b) => b.text() === "预览")!
    .trigger("click");
  expect(w.emitted("preview")).toBeTruthy();
  expect(w.emitted("preview")![0]).toEqual([
    "/console/v1/admin/templates/home/custom/versions/5/preview",
    "首页自定义 HTML 第 5 版",
  ]);
});
