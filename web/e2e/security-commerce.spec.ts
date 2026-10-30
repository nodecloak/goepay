import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("TOTP setup shows a local QR and confirms before revoking the displayed session", async ({
  page,
}) => {
  let confirmed = false;
  await page.route("**/console/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "token",
        }),
      );
    if (path.endsWith("/prepare")) {
      expect(route.request().postDataJSON()).toEqual({
        current_password: "test-password-12",
      });
      return route.fulfill(
        ok({
          secret: "JBSWY3DPEHPK3PXP",
          otpauth_uri: "otpauth://totp/Test?secret=JBSWY3DPEHPK3PXP",
          expires_at: "2026-10-23T12:00:00+08:00",
        }),
      );
    }
    if (path.endsWith("/confirm")) {
      confirmed = true;
      expect(route.request().postDataJSON()).toEqual({ code: "123456" });
      return route.fulfill(ok({ enabled: true }));
    }
    return route.fulfill(ok({ enabled: false }));
  });
  await page.goto("/admin/system/change-password");
  await page.getByRole("button", { name: "绑定身份验证器" }).click();
  await page.getByLabel("当前登录密码").fill("test-password-12");
  await page.getByRole("button", { name: "验证密码并生成绑定信息" }).click();
  await expect(page.getByAltText("身份验证器绑定二维码")).toBeVisible();
  await expect(page.getByLabel("手动绑定密钥")).toHaveValue("JBSWY3DPEHPK3PXP");
  expect(confirmed).toBe(false);
  await page.getByLabel("6 位动态验证码").fill("123456");
  await page.getByRole("button", { name: "确认绑定", exact: true }).click();
  await expect(page.getByRole("button", { name: "登录工作台" })).toBeVisible();
  await expect(page.getByRole("status")).toHaveText(
    "身份验证设置已更新，请重新登录",
  );
  expect(
    await page.evaluate(
      () =>
        Object.keys(localStorage).length + Object.keys(sessionStorage).length,
    ),
  ).toBe(0);
});
test("membership purchase uses selected server group and no client supplied price", async ({
  page,
}) => {
  let submitted: unknown;
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "token",
        }),
      );
    if (p.endsWith("/membership-sales"))
      return route.fulfill(
        ok({
          items: [
            {
              id: "1",
              name: "标准会员",
              price_cents: "100",
              duration_days: 30,
              duration_months: null,
              permanent: false,
            },
          ],
          total: 1,
        }),
      );
    if (req.method() === "POST") {
      submitted = req.postDataJSON();
      return route.fulfill(ok({ price: "1.00" }));
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/account/groupbuy");
  await page.getByRole("button", { name: "购买会员", exact: true }).click();
  const buyDialog = page.getByRole("dialog");
  // 购买用户组 is now a KitSelect (custom listbox): open the trigger, then pick
  // the server-priced option. The price/period come from the server catalog;
  // the client never supplies a price.
  await buyDialog
    .getByRole("button", { name: "购买用户组", exact: true })
    .click();
  await buyDialog
    .getByRole("option", { name: "标准会员 · 1.00 元 / 30天" })
    .click();
  await page.getByLabel("业务请求号").fill("membership-request");
  await page.getByRole("button", { name: "确认购买会员" }).click();
  await expect(page.locator(".alert.success")).toContainText("会员购买已完成");
  expect(submitted).toEqual({
    group_id: "1",
    external_no: "membership-request",
    quantity: 1,
  });
});
test("recharge creates an unpaid order and never follows a non-HTTP payment URL", async ({
  page,
}) => {
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "token",
        }),
      );
    if (req.method() === "POST") {
      expect(req.postDataJSON()).toMatchObject({
        amount: "0.01",
        type: "alipay",
        method: "page",
      });
      return route.fulfill(
        ok({
          trade_no: "RECHARGE-1",
          status: "pending",
          amount: "0.01",
          pay_url: "javascript:alert(1)",
        }),
      );
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/account/recharge");
  await page.getByRole("button", { name: "余额充值", exact: true }).click();
  await page.getByLabel("金额（元）").fill("0.01");
  await page.getByRole("button", { name: "确认余额充值" }).click();
  await expect(page.locator(".alert.success")).toContainText(
    "仅在支付成功并核实回调后才会入账",
  );
  await expect(page.getByRole("link", { name: "前往收银台" })).toHaveCount(0);
  await expect(
    page.getByText("订单号 RECHARGE-1 · 状态 pending"),
  ).toBeVisible();
});
test("browser recharge exposes supported browser scenes and rejects injected SDK or unknown scenes", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "M", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (route.request().method() === "POST") writes++;
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/account/recharge");
  await page.getByRole("button", { name: "余额充值", exact: true }).click();
  const dialog = page.getByRole("dialog");
  // 支付场景 and 支付平台 are now KitSelect custom listboxes (aria-label on the
  // trigger button, role=option items only when opened). The old native-select
  // DOM injection of app/jsapi/unknown scenes is no longer possible from the
  // UI; the fail-closed guarantee is now that the listbox offers ONLY the
  // scenes supported for each platform, and that no unsupported scene can be
  // chosen, with no write happening while the merchant explores.
  const sceneTrigger = dialog.getByRole("button", {
    name: "支付场景",
    exact: true,
  });
  const sceneOptions = async () => {
    await sceneTrigger.click();
    const labels = await dialog
      .getByRole("listbox")
      .getByRole("option")
      .allInnerTexts();
    // close the listbox again without changing the value (toggle the trigger)
    await sceneTrigger.click();
    await expect(dialog.getByRole("listbox")).toHaveCount(0);
    return labels.map((t) => t.trim());
  };
  expect(await sceneOptions()).toEqual(["page", "wap"]);
  await dialog.getByRole("button", { name: "支付平台", exact: true }).click();
  await dialog.getByRole("option", { name: "微信支付", exact: true }).click();
  expect(await sceneOptions()).toEqual(["native", "h5"]);
  // No app/jsapi/unknown scene is ever offered, and exploring the dialog writes
  // nothing to the server.
  for (const forbidden of ["app", "jsapi", "unknown"]) {
    await sceneTrigger.click();
    await expect(
      dialog.getByRole("listbox").getByRole("option", { name: forbidden }),
    ).toHaveCount(0);
    await sceneTrigger.click();
  }
  expect(writes).toBe(0);
});
