import { test, expect } from "@playwright/test";
import { localRedBackgroundPNG } from "./local-test-images";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
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
          ? [
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
            ].map((name) => ({
              name,
              title: name === "default" ? "标准模板" : name,
              description: "本地布局",
            }))
          : [
              {
                name: "default",
                title: "标准模板",
                description: "本地标准布局",
              },
              {
                name: "compact",
                title: "紧凑模板",
                description: "本地紧凑布局",
              },
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
          items: [
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
          ].map((name) => ({ name, title: name, description: "本地" })),
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
          ? [
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
            ].map((name) => ({ name, title: name, description: "本地" }))
          : [
              { name: "default", title: "标准", description: "本地" },
              { name: "compact", title: "紧凑", description: "本地" },
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
