import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const width of [375, 1280]) {
  test(`merchant can inspect pending review but cannot edit pay in account profile at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let saves = 0;
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "82", name: "待审核商户", role: "merchant" },
            csrf_token: "mock-review82",
          }),
        );
      if (p.endsWith("/account/profile")) {
        if (req.method() === "PATCH") {
          saves++;
          expect(req.postDataJSON()).toEqual({
            name: "待审核商户",
            email: "review82@example.test",
          });
        }
        return route.fulfill(
          ok({ name: "待审核商户", email: "review82@example.test", pay: "2" }),
        );
      }
      return route.fulfill(ok({ items: [], total: 0 }));
    });
    await page.goto("/merchant/account/settings");
    await expect(
      page.getByText("收款状态：待审核", { exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("combobox")).toHaveCount(0);
    await page
      .getByRole("button", { name: "保存账户资料", exact: true })
      .click();
    await expect(page.getByRole("status")).toContainText("账户资料已保存");
    expect(saves).toBe(1);
    await page.screenshot({
      path: test.info().outputPath(`merchant-review-${width}.png`),
      fullPage: true,
    });
  });
  test(`original pay field and Chinese review decision remain separate from account enabled at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let pay = "2",
      writes = 0;
    const merchant = () => ({
      id: "82",
      name: "待审核商户",
      enabled: true,
      pay,
    });
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "管理员",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "mock-review82",
          }),
        );
      if (p.endsWith("/schema"))
        return route.fulfill(
          ok({
            title: "商户管理",
            fields: [
              { name: "id", label: "商户ID", type: "string", read_only: true },
              { name: "name", label: "名称", type: "string" },
              { name: "enabled", label: "账户状态", type: "boolean" },
              {
                name: "pay",
                label: "收款状态",
                type: "select",
                default: "1",
                options: [
                  { label: "禁止收款", value: "0" },
                  { label: "正常", value: "1" },
                  { label: "待审核", value: "2" },
                ],
              },
            ],
            permissions: { create: false, update: true, delete: false },
          }),
        );
      if (p.endsWith("/82")) {
        if (req.method() === "PATCH") {
          writes++;
          expect(req.headers()["x-csrf-token"]).toBe("mock-review82");
          expect(req.postDataJSON().pay).toBe("1");
          expect(req.postDataJSON().enabled).toBe(true);
          pay = "1";
        }
        return route.fulfill(ok(merchant()));
      }
      return route.fulfill(ok({ items: [merchant()], total: 1 }));
    });
    await page.goto("/admin/merchant/merchant-list");
    await expect(
      page.getByRole("columnheader", { name: "收款状态", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "待审核", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByLabel("账户状态", { exact: true })).toBeChecked();
    await expect(dialog.getByRole("combobox", { name: "收款状态", exact: true })).toHaveValue(
      "2",
    );
    await dialog.getByRole("combobox", { name: "收款状态", exact: true }).selectOption("1");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(writes).toBe(1);
    await expect(
      page.getByRole("cell", { name: "待审核", exact: true }),
    ).toHaveCount(0);
    await page.screenshot({
      path: test.info().outputPath(`pay-review-${width}.png`),
      fullPage: true,
    });
  });
  test(`verified registration displays pending review and never auto logs in at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    let logins = 0;
    await page.route("**/console/v1/**", (route) => {
      const p = new URL(route.request().url()).pathname;
      if (p.endsWith("/session"))
        return route.fulfill({
          status: 401,
          contentType: "application/json",
          body: '{"code":-1}',
        });
      if (p.endsWith("/capabilities"))
        return route.fulfill(
          ok({
            registration_open: true,
            email_enabled: true,
            invite_required: false,
            review_required: true,
            expires_seconds: 600,
          }),
        );
      if (p.endsWith("/verification-code"))
        return route.fulfill(
          ok(
            {
              challenge_id: "opaque-review82",
              expires_at: new Date(Date.now() + 600000).toISOString(),
            },
            202,
          ),
        );
      if (p.endsWith("/register")) {
        expect(route.request().postDataJSON().pay).toBeUndefined();
        return route.fulfill(
          ok({ merchant_id: "82", username: "review82", pay: "2" }, 201),
        );
      }
      if (p.endsWith("/login")) logins++;
      return route.fulfill(ok(null));
    });
    await page.goto("/merchant/dashboard");
    await page.getByRole("button", { name: "注册商户", exact: true }).click();
    await expect(
      page.getByText(
        "新注册商户的收款权限须经管理员审核；注册后可登录查看状态。",
        { exact: true },
      ),
    ).toBeVisible();
    await page
      .getByLabel("邮箱", { exact: true })
      .fill("review82@example.test");
    await page.getByRole("button", { name: "发送验证码", exact: true }).click();
    await page.getByLabel("用户名", { exact: true }).fill("review82");
    await page.getByLabel("商户名称", { exact: true }).fill("待审核商户");
    await page
      .getByLabel("新密码", { exact: true })
      .fill("review82-test-password");
    await page
      .getByLabel("确认新密码", { exact: true })
      .fill("review82-test-password");
    await page.getByLabel("邮箱验证码", { exact: true }).fill("1234567");
    await page.getByRole("button", { name: "完成注册", exact: true }).click();
    await expect(page.getByRole("status")).toContainText("收款权限待审核");
    expect(logins).toBe(0);
    await expect(page.getByLabel("新密码", { exact: true })).toHaveCount(0);
    await page.screenshot({
      path: test.info().outputPath(`registration-review-${width}.png`),
      fullPage: true,
    });
  });
}

