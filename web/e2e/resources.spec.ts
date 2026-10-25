import { test, expect } from "@playwright/test";
test("resource schema drives validated create edit detail and delete", async ({
  page,
}) => {
  let item: Record<string, unknown> | null = null;
  const ok = (data: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify({ code: 0, data }),
  });
  await page.route("**/console/v1/**", async (route) => {
    const r = route.request(),
      path = new URL(r.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf-resource",
        }),
      );
    if (path.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "授权域名",
          description: "管理允许接入的业务域名",
          fields: [
            {
              name: "domain",
              label: "域名",
              type: "string",
              required: true,
              max_length: 253,
            },
            { name: "enabled", label: "启用", type: "boolean" },
          ],
          permissions: { create: true, update: true, delete: true },
        }),
      );
    if (r.method() === "POST") {
      expect(r.headers()["x-csrf-token"]).toBe("csrf-resource");
      item = { id: "d1", ...r.postDataJSON() };
      return route.fulfill(ok(item));
    }
    if (r.method() === "PATCH") {
      item = { ...item, ...r.postDataJSON() };
      return route.fulfill(ok(item));
    }
    if (r.method() === "DELETE") {
      item = null;
      return route.fulfill(ok({}));
    }
    return route.fulfill(
      ok(
        path.endsWith("/d1")
          ? item
          : { items: item ? [item] : [], total: item ? 1 : 0 },
      ),
    );
  });
  await page.goto("/admin/merchant/domain");
  await page.getByRole("button", { name: "新增授权域名" }).click();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("请填写域名")).toBeVisible();
  await page
    .getByRole("dialog")
    .getByLabel("域名", { exact: false })
    .fill("pay.example.com");
  await page.getByLabel("启用", { exact: true }).check();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "pay.example.com" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByLabel("域名", { exact: false })
    .fill("new.example.com");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "new.example.com" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("new.example.com");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "删除", exact: true }).click();
  await page.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(page.getByText("暂无记录，可通过上方操作添加")).toBeVisible();
  expect(item).toBeNull();
});
test("read-only schemas never expose write operations", async ({ page }) => {
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        code: 0,
        data: p.endsWith("/session")
          ? { user: { id: "1", name: "M", role: "merchant" }, csrf_token: "x" }
          : p.endsWith("/schema")
            ? {
                title: "授权域名",
                fields: [],
                permissions: { create: false, update: false, delete: false },
              }
            : { items: [{ id: "1" }], total: 1 },
      }),
    });
  });
  await page.goto("/merchant/domain");
  await expect(
    page.getByRole("button", { name: "详情", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: /新增|编辑|删除/ }),
  ).toHaveCount(0);
});
