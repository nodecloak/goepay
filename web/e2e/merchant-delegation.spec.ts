import { test, expect } from "@playwright/test";

test("missing merchant login opens the existing login editor without consuming a grant or checking an old merchant session", async ({
  page,
}) => {
  const writes: string[] = [];
  let merchantSessionReads = 0;
  const merchant = {
    id: "10002",
    name: "API merchant",
    enabled: true,
    login_username: "",
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    const ok = (data: unknown) =>
      route.fulfill({ status: 200, json: { code: 0, data } });
    if (req.method() !== "GET") {
      writes.push(p);
      if (p.endsWith("/sso/grants"))
        return route.fulfill({
          status: 409,
          json: {
            code: -1,
            message: "enabled merchant login account required",
          },
        });
      throw Error("Unexpected write: " + p);
    }
    if (p.includes("/merchant/session")) {
      merchantSessionReads++;
      return route.fulfill({ status: 401, json: { code: -1 } });
    }
    if (p.endsWith("/session"))
      return ok({
        user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
        csrf_token: "admin-csrf",
      });
    if (p.endsWith("/security/payment-password"))
      return ok({
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      });
    if (p.endsWith("/schema"))
      return ok({
        title: "商户列表",
        fields: [
          { name: "name", label: "名称", type: "string" },
          { name: "login_username", label: "后台登录账号", type: "string" },
          {
            name: "login_password",
            label: "后台登录密码",
            type: "password",
            write_only: true,
          },
        ],
        permissions: { create: false, update: true, delete: false },
        actions: ["sso"],
      });
    if (p.endsWith("/resources/merchants/10002")) return ok(merchant);
    return ok({ items: [merchant], total: 1 });
  });
  await page.goto("/admin/merchant/merchant-list");
  await page
    .getByRole("button", { name: "商户关联记录 10002", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "一键登录", exact: true }).click();
  await page
    .getByRole("button", { name: "确认建立代登录会话", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "确认代登录", exact: true });
  await expect(dialog.getByRole("alert")).toContainText(
    "尚未配置可用的后台登录账号",
  );
  await expect(dialog).not.toContainText("结果尚未核实");
  await dialog
    .getByRole("button", { name: "设置后台登录", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByLabel("后台登录账号", { exact: true })).toBeVisible();
  await expect(page.getByLabel("后台登录密码", { exact: true })).toBeVisible();
  expect(writes).toEqual(["/console/v1/admin/sso/grants"]);
  expect(merchantSessionReads).toBe(0);
});
