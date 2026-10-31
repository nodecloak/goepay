import { test, expect } from "@playwright/test";
import { localRedBackgroundPNG } from "./local-test-images";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const err = (message: string, status = 422) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 1, message }),
});
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
const builtin = (name: string, title: string, description: string) => ({
  name,
  title,
  description,
  source: "builtin",
});
const fields = [
  {
    name: "brand_name",
    label: "品牌名称",
    type: "text",
    required: true,
    max_length: 64,
  },
  {
    name: "notice",
    label: "页面说明",
    type: "textarea",
    required: false,
    max_length: 240,
  },
  { name: "accent_color", label: "强调色", type: "color", required: true },
  { name: "background_color", label: "背景色", type: "color", required: true },
  { name: "logo_asset_id", label: "品牌标志", type: "image", required: false },
  { name: "density", label: "间距", type: "select", required: true },
  {
    name: "show_order_name",
    label: "显示订单名称",
    type: "switch",
    required: true,
  },
];
const cfg = {
  brand_name: "GOEPAY",
  notice: "Local",
  accent_color: "#007c91",
  background_color: "#f4f8fa",
  logo_asset_id: "",
  density: "comfortable",
  show_order_name: true,
};
test("typed templates require explicit confirmation and kind-wide CAS, private image and sandbox preview", async ({
  page,
}) => {
  let revision = "0",
    selected = "default";
  const writes: { path: string; body: any }[] = [],
    saved: Record<string, any> = { default: { ...cfg }, compact: { ...cfg } },
    paths: string[] = [];
  await page.route("**/console/v1/**", (r) => {
    const request = r.request(),
      url = new URL(request.url()),
      p = url.pathname;
    paths.push(p);
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/templates")) {
      const kind = url.searchParams.get("type");
      const items =
        kind === "home"
          ? homeNames.map((name) =>
              builtin(name, name === "default" ? "标准模板" : name, "本地布局"),
            )
          : [
              builtin("default", "标准模板", "本地标准布局"),
              builtin("compact", "紧凑模板", "本地紧凑布局"),
              ...(kind === "pay"
                ? [builtin("anime", "二次元模板", "本地二次元布局")]
                : []),
            ];
      return r.fulfill(ok({ type: kind, revision, selected, items }));
    }
    if (p.endsWith("/selection")) {
      const body = request.postDataJSON();
      writes.push({ path: p, body });
      selected = body.name;
      revision = String(Number(revision) + 1);
      return r.fulfill(ok({}));
    }
    if (p.endsWith("/preview"))
      return r.fulfill({
        contentType: "text/html",
        body: "<p>PREVIEW-ONLY</p>",
      });
    const m = p.match(/\/templates\/(home|pay|qrpay)\/([a-z0-9]+)\/config$/);
    if (m) {
      const kind = m[1] as "home" | "pay" | "qrpay";
      if (request.method() === "PUT") {
        const body = request.postDataJSON();
        writes.push({ path: p, body });
        saved[m[2]] = body.config;
        revision = String(Number(revision) + 1);
      }
      const base = { ...(saved[m[2]] ?? cfg) };
      if (kind !== "pay") delete base.show_order_name;
      return r.fulfill(
        ok({
          type: kind,
          name: m[2],
          revision,
          config: base,
          fields: fields.filter(
            (f) => kind === "pay" || f.name !== "show_order_name",
          ),
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/app/template");
  await expect(
    page.getByRole("button", { name: "检查并保存模板设置" }),
  ).toBeVisible();
  // The console now opens on 首页模板; the typed default/compact card flow
  // belongs to the 支付页模板 kind.
  await page.getByRole("button", { name: "支付页模板", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "支付收银台 / default" }),
  ).toBeVisible();
  await page
    .getByLabel("品牌名称", { exact: true })
    .fill("<img src=x onerror=1> brand");
  await page.getByRole("button", { name: "检查并保存模板设置" }).click();
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(
    page.getByText("模板设置已保存；仅当前选中的模板影响公开页面。"),
  ).toBeVisible();
  expect(writes[0].body.expected_revision).toBe("0");
  expect(writes[0].body.config.brand_name).toBe("<img src=x onerror=1> brand");
  await page.getByRole("button", { name: /紧凑模板.*标识：compact/ }).click();
  await page.getByRole("button", { name: "检查并设为当前模板" }).click();
  expect(writes).toHaveLength(1);
  await page.getByRole("button", { name: "确认切换", exact: true }).click();
  await expect(
    page.getByText("模板已切换；后续页面使用新外观。"),
  ).toBeVisible();
  await expect(page.getByText(/当前使用：.*（compact）/)).toBeVisible();
  expect(writes[1].body).toEqual({ expected_revision: "1", name: "compact" });
  await page.getByRole("button", { name: "预览已保存配置" }).click();
  await expect(page.locator("iframe")).toHaveAttribute(
    "sandbox",
    "allow-same-origin",
  );
  await expect(page.locator("iframe")).toHaveAttribute(
    "src",
    "/console/v1/admin/templates/pay/compact/preview",
  );
  expect(paths.some((p) => p.includes("/resources/"))).toBe(false);
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
test("template unknown response blocks repeated mutation until explicit reload and operators cannot call API", async ({
  page,
}) => {
  let writes = 0,
    operator = false;
  await page.route("**/console/v1/**", (r) => {
    const request = r.request(),
      p = new URL(request.url()).pathname;
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: {
            id: "1",
            name: "Admin",
            role: "admin",
            admin_class: operator ? "operator" : "super",
          },
          csrf_token: "c",
        }),
      );
    if (request.method() === "PUT") {
      writes++;
      return r.abort("failed");
    }
    if (p.endsWith("/templates")) {
      return r.fulfill(
        ok({
          type: "home",
          revision: "0",
          selected: "default",
          items: homeNames.map((name) => builtin(name, name, "本地")),
        }),
      );
    }
    if (p.endsWith("/config")) {
      const homeCfg: any = { ...cfg };
      delete homeCfg.show_order_name;
      return r.fulfill(
        ok({
          type: "home",
          name: "default",
          revision: "0",
          config: homeCfg,
          fields: fields.filter((f) => f.name !== "show_order_name"),
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/app/template");
  await page.getByRole("button", { name: "检查并保存模板设置" }).click();
  await page.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("禁止自动重发");
  await expect(
    page.getByRole("button", { name: "检查并保存模板设置" }),
  ).toBeDisabled();
  expect(writes).toBe(1);
  await page.getByRole("button", { name: "重新读取模板" }).click();
  await expect(
    page.getByRole("button", { name: "检查并保存模板设置" }),
  ).toBeEnabled();
  operator = true;
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "您没有访问此页面的权限" }),
  ).toBeVisible();
  expect(writes).toBe(1);
});

test("three template kinds keep typed fields and image upload stays a draft until explicit save", async ({
  page,
}) => {
  const assetID = "a".repeat(64),
    writes: unknown[] = [],
    uploads: string[] = [];
  const image = localRedBackgroundPNG();
  await page.route("**/console/v1/**", (r) => {
    const request = r.request(),
      url = new URL(request.url()),
      p = url.pathname;
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/templates")) {
      const kind = url.searchParams.get("type");
      const items =
        kind === "home"
          ? homeNames.map((name) => builtin(name, name, "本地"))
          : [
              builtin("default", "标准", "本地"),
              builtin("compact", "紧凑", "本地"),
              ...(kind === "pay" ? [builtin("anime", "二次元", "本地")] : []),
            ];
      return r.fulfill(
        ok({ type: kind, revision: "0", selected: "default", items }),
      );
    }
    if (p.endsWith("/images") && request.method() === "POST") {
      uploads.push(request.headers()["x-csrf-token"]);
      return r.fulfill(
        ok({ id: assetID, mime: "image/png", width: 1, height: 1 }, 201),
      );
    }
    if (p.endsWith("/images/" + assetID))
      return r.fulfill({ contentType: "image/png", body: image });
    const m = p.match(/\/templates\/(home|pay|qrpay)\/default\/config$/);
    if (m) {
      const config: any = { ...cfg };
      if (m[1] !== "pay") delete config.show_order_name;
      if (request.method() === "PUT") writes.push(request.postDataJSON());
      return r.fulfill(
        ok({
          type: m[1],
          name: "default",
          revision: "0",
          config,
          fields: fields.filter(
            (f) => m[1] === "pay" || f.name !== "show_order_name",
          ),
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/app/template");
  // The console opens on 首页模板, which has no 显示订单名称 switch.
  await expect(
    page.getByRole("heading", { name: "首页 / default" }),
  ).toBeVisible();
  await expect(page.getByLabel("显示订单名称")).toHaveCount(0);
  await page.getByRole("button", { name: "支付页模板", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "支付收银台 / default" }),
  ).toBeVisible();
  await expect(page.getByLabel("显示订单名称")).toBeVisible();
  await page
    .getByRole("button", { name: "聚合收款页模板", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "聚合收款页 / default" }),
  ).toBeVisible();
  await expect(page.getByLabel("显示订单名称")).toHaveCount(0);
  await page
    .getByLabel(/品牌标志/)
    .setInputFiles({ name: "local.png", mimeType: "image/png", buffer: image });
  await expect(page.getByText(/图片已私有入库/)).toBeVisible();
  await expect(page.getByAltText("当前草稿标志")).toHaveAttribute(
    "src",
    "/console/v1/admin/templates/images/" + assetID,
  );
  expect(uploads).toEqual(["c"]);
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "检查并保存模板设置" }).click();
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({
    expected_revision: "0",
    config: {
      brand_name: "GOEPAY",
      notice: "Local",
      accent_color: "#007c91",
      background_color: "#f4f8fa",
      logo_asset_id: assetID,
      density: "comfortable",
    },
  });
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toContain(assetID);
});

const fileKey = "0123456789abcdef0123456789abcdef";
test("uploading a home template package confirms, lists versions, and restores with CAS", async ({
  page,
}) => {
  let revision = "5",
    uploaded = false,
    uploadVersion = 2;
  const uploadItem = () => ({
    name: "my-style",
    title: "我的模板",
    description: "",
    source: "upload",
    picture_url: `/template-files/${fileKey}/preview.png`,
    upload_version: uploadVersion,
    updated_at: "2026-10-31T10:20:05Z",
    updated_by: "admin",
    files: 7,
    size: 2048,
  });
  const versions = () =>
    Array.from({ length: uploadVersion }, (_, i) => ({
      version: uploadVersion - i,
      operation: uploadVersion - i === uploadVersion ? "upload" : "upload",
      restored_from: 0,
      title: "我的模板",
      package_version: "1.0",
      author: "me",
      files: 7,
      size: 2048,
      created_at: "2026-10-31T10:20:05Z",
      actor: "admin",
      notice: uploadVersion - i === uploadVersion ? "pending" : "accepted",
    }));
  await page.route("**/console/v1/**", (r) => {
    const request = r.request(),
      url = new URL(request.url()),
      p = url.pathname;
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/uploads/check"))
      return r.fulfill(
        ok({
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
        }),
      );
    if (p.endsWith("/uploads")) {
      uploaded = true;
      uploadVersion = 2;
      revision = String(Number(revision) + 1);
      return r.fulfill(
        ok(
          {
            type: "home",
            name: "my-style",
            title: "我的模板",
            version: 2,
            revision,
            created: true,
            selected: false,
            files: 7,
            size: 2048,
            warnings: [],
            removed: {},
            ignored: [],
          },
          201,
        ),
      );
    }
    if (p.endsWith("/my-style/versions"))
      return r.fulfill(ok({ items: versions() }));
    if (p.includes("/my-style/versions/") && p.endsWith("/restore")) {
      revision = String(Number(revision) + 1);
      uploadVersion += 1;
      return r.fulfill(ok({ version: uploadVersion, revision }));
    }
    if (p.endsWith("/preview"))
      return r.fulfill({ contentType: "text/html", body: "<p>PREVIEW</p>" });
    if (p.endsWith("/templates")) {
      const kind = url.searchParams.get("type");
      const base = homeNames.map((name) => builtin(name, name, "本地"));
      const items =
        kind === "home" && uploaded ? [...base, uploadItem()] : base;
      return r.fulfill(
        ok({
          type: kind,
          revision,
          selected: "default",
          items: kind === "home" ? items : items.slice(0, 2),
        }),
      );
    }
    const m = p.match(/\/templates\/(home|pay|qrpay)\/([a-z0-9-]+)\/config$/);
    if (m) {
      const config: any = { ...cfg };
      delete config.show_order_name;
      return r.fulfill(
        ok({
          type: m[1],
          name: m[2],
          revision,
          config,
          fields: fields.filter((f) => f.name !== "show_order_name"),
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/app/template");
  await expect(
    page.getByRole("button", { name: "检查并保存模板设置" }),
  ).toBeVisible();
  // Upload a package: the hidden picker fires the check, then the dialog.
  await page.setInputFiles("input.template-package-input", {
    name: "pkg.zip",
    mimeType: "application/zip",
    buffer: Buffer.from([1, 2, 3]),
  });
  await expect(page.getByText("我的模板（my-style）")).toBeVisible();
  await expect(page.getByText("新模板，第 1 版")).toBeVisible();
  await expect(page.getByText("未知占位符 {{x}}")).toBeVisible();
  await expect(page.getByText("README.md")).toBeVisible();
  await page.getByRole("button", { name: "确认上传", exact: true }).click();
  await expect(page.getByText(/已上传模板「我的模板」/)).toBeVisible();
  // The uploaded card is shown with the 上传 tag.
  await expect(
    page.getByRole("button", { name: /我的模板.*标识：my-style/ }),
  ).toBeVisible();
  // Open the versions dialog from the uploaded card.
  await page.getByRole("button", { name: "版本", exact: true }).click();
  await expect(page.getByText("等待发送")).toBeVisible();
  await expect(page.getByText("已提交")).toBeVisible();
  // The newest version offers no 恢复; restore an older one with the CAS body.
  const restore = page
    .getByRole("row", { name: /第 1 版/ })
    .getByRole("button", { name: "恢复" });
  await restore.click();
  await page.getByRole("button", { name: "确认恢复", exact: true }).click();
  await expect(page.getByText(/恢复为第 1 版/)).toBeVisible();
  // Preview the uploaded home template: it runs scripts, never same-origin.
  await page.getByRole("button", { name: "预览 我的模板" }).click();
  const pane = page.locator(".template-fullpreview");
  const sandbox = await pane.locator("iframe").getAttribute("sandbox");
  expect(sandbox).toContain("allow-scripts");
  expect(sandbox).not.toContain("allow-same-origin");
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("an upload the server rejects shows its 422 message verbatim", async ({
  page,
}) => {
  await page.route("**/console/v1/**", (r) => {
    const request = r.request(),
      url = new URL(request.url()),
      p = url.pathname;
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/uploads/check"))
      return r.fulfill(err("支付页和收款页不运行脚本"));
    if (p.endsWith("/templates")) {
      const kind = url.searchParams.get("type");
      const items =
        kind === "home"
          ? homeNames.map((name) => builtin(name, name, "本地"))
          : [
              builtin("default", "标准", "本地"),
              builtin("compact", "紧凑", "本地"),
            ];
      return r.fulfill(
        ok({ type: kind, revision: "0", selected: "default", items }),
      );
    }
    if (p.endsWith("/config")) {
      const config: any = { ...cfg };
      delete config.show_order_name;
      return r.fulfill(
        ok({
          type: "qrpay",
          name: "default",
          revision: "0",
          config,
          fields: fields.filter((f) => f.name !== "show_order_name"),
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/app/template");
  await page
    .getByRole("button", { name: "聚合收款页模板", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "聚合收款页 / default" }),
  ).toBeVisible();
  await page.setInputFiles("input.template-package-input", {
    name: "pkg.zip",
    mimeType: "application/zip",
    buffer: Buffer.from([1, 2, 3]),
  });
  await expect(page.getByRole("alert")).toContainText(
    "支付页和收款页不运行脚本",
  );
});
