// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import TemplatePage from "../src/components/TemplatePage.vue";
const homeNames = [
  "default",
  "index1",
  "index2",
  "index3",
  "index4",
  "index5",
  "index6",
  "index7",
  "index8",
  "index9",
  "index10",
  "minimal",
  "sakura",
  "starry",
  "manga",
  "mecha",
  "candy",
];
const builtin = (name: string, title = name, description = "本地") => ({
  name,
  title,
  description,
  source: "builtin",
});
const homeConfig = () => {
  const config: Record<string, string | boolean> = {
    brand_name: "Local",
    notice: "",
    accent_color: "#007c91",
    background_color: "#f4f8fa",
    logo_asset_id: "",
    density: "comfortable",
  };
  return config;
};
it("loads the typed template API for own super only and never generic template CRUD", async () => {
  const request = vi.fn((_path: string) =>
    Promise.resolve({
      type: "pay",
      revision: "0",
      selected: "default",
      items: [
        builtin("default", "标准"),
        builtin("compact", "紧凑"),
        builtin("anime", "二次元"),
      ],
    }),
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  expect(request.mock.calls[0]?.[0]).toBe("/templates?type=home");
  w.unmount();
  for (const p of [
    { adminClass: "operator", ownSession: true },
    { adminClass: "super", ownSession: false },
  ]) {
    request.mockClear();
    const v = mount(TemplatePage, {
      props: {
        client: Object.assign(new ApiClient("admin"), { request }),
        ...p,
      },
    });
    await flushPromises();
    expect(request).not.toHaveBeenCalled();
    v.unmount();
  }
});

it("preserves per-kind fields and rejects directory/config revision races before enabling writes", async () => {
  for (const kind of ["pay"]) {
    const config: Record<string, string | boolean> = {
      brand_name: "Local",
      notice: "",
      accent_color: "#007c91",
      background_color: "#f4f8fa",
      logo_asset_id: "",
      density: "comfortable",
    };
    if (kind === "pay") config.show_order_name = false;
    const request = vi.fn(async (path: string) =>
      path.includes("?")
        ? {
            type: kind,
            revision: "4",
            selected: "default",
            items: [
              builtin("default", "标准"),
              builtin("compact", "紧凑"),
              builtin("anime", "二次元"),
            ],
          }
        : { type: kind, name: "default", revision: "5", config, fields: [] },
    );
    const w = mount(TemplatePage, {
      props: {
        client: Object.assign(new ApiClient("admin"), { request }),
        adminClass: "super",
        ownSession: true,
      },
    });
    await flushPromises();
    // The page opens on 首页模板; this case is about the pay kind.
    await w
      .findAll("nav.template-tabs button")
      .find((b) => b.text() === "支付页模板")!
      .trigger("click");
    await flushPromises();
    expect(w.find("form").exists()).toBe(false);
    expect(w.find("[role=alert]").exists()).toBe(true);
    w.unmount();
  }
});
it("does not continue configuration reads after unmount or revive a stale page", async () => {
  let resolve!: (value: unknown) => void;
  const request = vi.fn(
    (_path: string) =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  w.unmount();
  resolve({
    type: "pay",
    revision: "0",
    selected: "default",
    items: [
      builtin("default", "标准"),
      builtin("compact", "紧凑"),
      builtin("anime", "二次元"),
    ],
  });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
});


it("shows each template's preview picture on its card", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("?")
      ? {
          type: "pay",
          revision: "0",
          selected: "default",
          items: [
            builtin("default", "默认支付页模板"),
            builtin("compact", "紧凑支付页模板"),
            builtin("anime", "二次元支付页模板"),
          ],
        }
      : {
          type: "pay",
          name: "default",
          revision: "0",
          config: {
            brand_name: "Local",
            notice: "",
            accent_color: "#007c91",
            background_color: "#f4f8fa",
            logo_asset_id: "",
            density: "comfortable",
            show_order_name: false,
          },
          fields: [],
        },
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  // The page opens on 首页模板; switch to the pay kind this mock serves.
  await w
    .findAll("nav.template-tabs button")
    .find((b) => b.text() === "支付页模板")!
    .trigger("click");
  await flushPromises();
  expect(
    w.findAll("img.template-thumb").map((i) => i.attributes("src")),
  ).toEqual([
    "/static/template-previews/pay-default.jpg",
    "/static/template-previews/pay-compact.jpg",
    "/static/template-previews/pay-anime.jpg",
  ]);
  // The card's 预览 button opens the template over the main content area.
  await w.get('button[aria-label="预览 紧凑支付页模板"]').trigger("click");
  const pane = w.get(".template-fullpreview");
  expect(pane.attributes("role")).toBe("dialog");
  expect(pane.get("iframe").attributes("src")).toBe(
    "/console/v1/admin/templates/pay/compact/preview",
  );
  // A built-in pay template is same-origin in the preview iframe.
  expect(pane.get("iframe").attributes("sandbox")).toBe("allow-same-origin");
  expect(pane.text()).toContain("紧凑支付页模板");
  await pane.get('button[aria-label="关闭预览"]').trigger("click");
  expect(w.find(".template-fullpreview").exists()).toBe(false);
  // Esc closes it too, and opening a preview does not change the selection.
  await w.get('button[aria-label="预览 默认支付页模板"]').trigger("click");
  await w.get(".template-fullpreview").trigger("keydown", { key: "Escape" });
  expect(w.find(".template-fullpreview").exists()).toBe(false);
  expect(request.mock.calls.every((c) => !String(c[0]).includes("/select"))).toBe(
    true,
  );
  w.unmount();
});


it("opens on the 首页模板 tab with the 17 built-in home styles", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("?")
      ? {
          type: "home",
          revision: "0",
          selected: "default",
          items: homeNames.map((name) => builtin(name)),
        }
      : {
          type: "home",
          name: "default",
          revision: "0",
          config: homeConfig(),
          fields: [],
        },
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  const pressed = w.find('nav.template-tabs button[aria-pressed="true"]');
  expect(pressed.text()).toBe("首页模板");
  expect(w.findAll("img.template-thumb").length).toBe(17);
  // The upload toolbar and the sample download are shown.
  expect(w.findAll("button").some((b) => b.text().includes("上传模板"))).toBe(
    true,
  );
  const sample = w.get("a.template-sample");
  expect(sample.attributes("href")).toBe(
    "/console/v1/admin/templates/home/sample",
  );
  w.unmount();
});

it("uploads a package after a confirmation and reloads the list, running scripts for a home upload preview", async () => {
  const key = "0123456789abcdef0123456789abcdef";
  let uploaded = false;
  const uploadItem = () => ({
    name: "my-style",
    title: "我的模板",
    description: "",
    source: "upload",
    picture_url: `/template-files/${key}/preview.png`,
    upload_version: 1,
    updated_at: "2026-10-31T10:20:05Z",
    updated_by: "admin",
    files: 7,
    size: 2048,
  });
  const posts: { path: string; body: any }[] = [];
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/uploads/check"))
      return {
        type: "home",
        name: "my-style",
        title: "我的模板",
        exists: false,
        selected: false,
        next_version: 1,
        identical: false,
        files: 7,
        size: 2048,
        warnings: ["未知占位符 {{x}}"],
        removed: {},
        ignored: ["README.md"],
      };
    if (path.endsWith("/uploads")) {
      posts.push({ path, body: options?.body });
      uploaded = true;
      return {
        type: "home",
        name: "my-style",
        title: "我的模板",
        version: 1,
        revision: "1",
        created: true,
        selected: false,
        files: 7,
        size: 2048,
        warnings: [],
        removed: {},
        ignored: [],
      };
    }
    if (path.includes("?"))
      return {
        type: "home",
        revision: uploaded ? "1" : "0",
        selected: "default",
        items: uploaded
          ? [...homeNames.map((n) => builtin(n)), uploadItem()]
          : homeNames.map((n) => builtin(n)),
      };
    const name = path.split("/")[3];
    return { type: "home", name, revision: uploaded ? "1" : "0", config: homeConfig(), fields: [] };
  });
  const client = Object.assign(new ApiClient("admin"), { request });
  // The multipart upload helpers go through fetch directly, not request().
  client.csrfToken = "c";
  const checkData = {
    type: "home",
    name: "my-style",
    title: "我的模板",
    exists: false,
    selected: false,
    next_version: 1,
    identical: false,
    files: 7,
    size: 2048,
    warnings: ["未知占位符 {{x}}"],
    removed: {},
    ignored: ["README.md"],
  };
  const uploadData = {
    type: "home",
    name: "my-style",
    title: "我的模板",
    version: 1,
    revision: "1",
    created: true,
    selected: false,
    files: 7,
    size: 2048,
    warnings: [],
    removed: {},
    ignored: [],
  };
  const fetchMock = vi.fn(async (url: string, _init?: any) => {
    if (String(url).endsWith("/uploads/check"))
      return {
        ok: true,
        status: 200,
        json: async () => ({ code: 0, data: checkData }),
      } as any;
    if (String(url).endsWith("/uploads")) {
      uploaded = true;
      return {
        ok: true,
        status: 201,
        json: async () => ({ code: 0, data: uploadData }),
      } as any;
    }
    throw new Error("unexpected fetch " + url);
  });
  vi.stubGlobal("fetch", fetchMock);
  const w = mount(TemplatePage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  // Choose a package: the hidden input fires the check.
  const file = new File([new Uint8Array([1, 2, 3])], "pkg.zip", {
    type: "application/zip",
  });
  const input = w.get("input.template-package-input")
    .element as HTMLInputElement;
  Object.defineProperty(input, "files", { value: [file], configurable: true });
  await w.get("input.template-package-input").trigger("change");
  for (let i = 0; i < 5; i++) await flushPromises();
  // The confirmation dialog shows the name, that it is new, and the warnings.
  const confirm = w.get(".template-upload-confirm");
  expect(confirm.text()).toContain("我的模板（my-style）");
  expect(confirm.text()).toContain("新模板，第 1 版");
  expect(confirm.text()).toContain("未知占位符 {{x}}");
  expect(confirm.text()).toContain("README.md");
  await confirm.get("button.primary").trigger("click");
  for (let i = 0; i < 5; i++) await flushPromises();
  // The upload POST carried expected_revision first then the file (checked via
  // the fetch body being FormData).
  expect(fetchMock).toHaveBeenCalledTimes(2);
  const uploadCall = fetchMock.mock.calls.find((c) =>
    String(c[0]).endsWith("/uploads"),
  )!;
  const body = (uploadCall[1] as any).body as FormData;
  expect([...body.keys()]).toEqual(["expected_revision", "file"]);
  expect(body.get("expected_revision")).toBe("0");
  expect(w.get("[role=status].success").text()).toContain("第 1 版");
  // The uploaded card is shown with the 上传 tag and its actions.
  expect(w.text()).toContain("我的模板");
  const actions = w.get(".template-card-actions");
  for (const label of ["设为当前", "配置", "版本", "删除"])
    expect(actions.text()).toContain(label);
  // Previewing the uploaded home template runs scripts, never same-origin.
  await w.get('button[aria-label="预览 我的模板"]').trigger("click");
  const pane = w.get(".template-fullpreview");
  const sandbox = pane.get("iframe").attributes("sandbox")!;
  expect(sandbox).toContain("allow-scripts");
  expect(sandbox).not.toContain("allow-same-origin");
  vi.unstubAllGlobals();
  w.unmount();
});

it("lists versions and blocks a repeated restore on a 409", async () => {
  const key = "0123456789abcdef0123456789abcdef";
  const uploadItem = {
    name: "my-style",
    title: "我的模板",
    description: "",
    source: "upload",
    picture_url: `/template-files/${key}/preview.png`,
    upload_version: 3,
    updated_at: "2026-10-31T10:20:05Z",
    updated_by: "admin",
    files: 7,
    size: 2048,
  };
  const request = vi.fn(async (path: string, options?: any) => {
    if (path.endsWith("/versions"))
      return {
        items: [
          {
            version: 3,
            operation: "upload",
            restored_from: 0,
            title: "我的模板",
            package_version: "1.2",
            author: "me",
            files: 7,
            size: 2048,
            created_at: "2026-10-31T10:20:05Z",
            actor: "admin",
            notice: "pending",
          },
          {
            version: 2,
            operation: "upload",
            restored_from: 0,
            title: "我的模板",
            package_version: "",
            author: "",
            files: 7,
            size: 2048,
            created_at: "2026-10-31T10:00:05Z",
            actor: "admin",
            notice: "accepted",
          },
        ],
      };
    if (path.includes("/restore")) throw new ApiError("版本冲突", 409, 0);
    if (path.includes("?"))
      return {
        type: "home",
        revision: "5",
        selected: "default",
        items: [...homeNames.map((n) => builtin(n)), uploadItem],
      };
    const name = path.split("/")[3];
    return { type: "home", name, revision: "5", config: homeConfig(), fields: [] };
  });
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  // Open the versions dialog from the uploaded card.
  await w
    .findAll(".template-card-actions button")
    .find((b) => b.text() === "版本")!
    .trigger("click");
  await flushPromises();
  const pane = w.get(".template-versions-pane");
  expect(pane.text()).toContain("第 3 版");
  expect(pane.text()).toContain("等待发送");
  expect(pane.text()).toContain("已提交");
  // The newest version offers no 恢复; an older one does.
  const rows = pane.findAll("tbody tr");
  expect(rows[0].findAll("button").map((b) => b.text())).toEqual([
    "预览",
    "下载",
  ]);
  const restore = rows[1].findAll("button").find((b) => b.text() === "恢复")!;
  await restore.trigger("click");
  await w.get(".template-versions-pane .template-confirm button.primary").trigger(
    "click",
  );
  await flushPromises();
  expect(w.get("[role=alert]").text()).toContain("版本已变化");
  w.unmount();
});
