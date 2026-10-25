import { test, expect } from "@playwright/test";
import { openAdminAPIRefund } from "./order-action-helpers";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("administrator refund requires masked preview, funds password and explicit original-body execution;202 does not resend", async ({
  page,
}) => {
  const submitted: unknown[] = [];
  let originalBody: Record<string, unknown> = {};
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/orders/AUTH") && r.method() === "GET")
      return route.fulfill(
        ok({
          order: { trade_no: "AUTH", status: "paid", amount: "100" },
          reporting: { refunded_amount: "0" },
          admin_control: { status: "", archived: false, revision: "0" },
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: true,
          enforced: true,
          credential_epoch: 1,
          totp_required: false,
        }),
      );
    if (p.endsWith("/funds-authorizations/challenges")) {
      expect(r.postDataJSON().target).toBe(
        "/console/v1/admin/orders/AUTH/refund",
      );
      originalBody = r.postDataJSON().body;
      expect(originalBody.amount).toBe("1.00");
      expect(originalBody.external_no).toBeTruthy();
      return route.fulfill(
        ok({
          challenge_id: "short-challenge",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: {
            kind: "refund",
            trade_no: "AUTH",
            amount: "1.00",
            recipient: "merchant***",
          },
        }),
      );
    }
    if (p.endsWith("/funds-authorizations/confirm")) {
      expect(r.postDataJSON()).toEqual({
        challenge_id: "short-challenge",
        payment_password: "Admin-funds-password",
      });
      return route.fulfill(
        ok({
          authorization: "once-only-grant",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    }
    if (p.endsWith("/refund")) {
      submitted.push(r.postDataJSON());
      expect(r.headers()["x-payment-authorization"]).toBe("once-only-grant");
      expect(r.headers()["x-csrf-token"]).toBe("csrf");
      return route.fulfill(ok({ status: "unknown" }, 202));
    }
    return route.fulfill(
      ok({
        items: [
          { trade_no: "AUTH", status: "paid", amount: "100", money: "1.00" },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "AUTH");
  await page.getByLabel("退款金额（元）", { exact: true }).fill("1.00");
  await page.getByRole("button", { name: "确认申请退款", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "管理员资金二次授权" });
  await expect(modal).toContainText("merchant***");
  expect(submitted).toEqual([]);
  await modal
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await modal
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("Admin-funds-password");
  await modal
    .getByRole("button", { name: "确认资金密码", exact: true })
    .click();
  await expect(
    modal.getByRole("button", { name: "执行已核对的原操作", exact: true }),
  ).toBeVisible();
  expect(submitted).toEqual([]);
  expect(await page.content()).not.toContain("once-only-grant");
  await modal
    .getByRole("button", { name: "执行已核对的原操作", exact: true })
    .click();
  await expect(modal).toHaveCount(0);
  expect(submitted).toEqual([originalBody]);
  await expect(page.getByRole("status")).toContainText("尚未核实");
  expect(
    await page.evaluate(() =>
      Object.keys(localStorage).concat(Object.keys(sessionStorage)),
    ),
  ).toEqual([]);
  expect(page.url()).not.toContain("grant");
});
test("administrator funds settings clear credentials on rejected update without storing them", async ({
  page,
}) => {
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "2",
            name: "Operator",
            role: "admin",
            admin_class: "operator",
          },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/security/payment-password")) {
      if (r.method() === "PUT") {
        writes++;
        expect(r.postDataJSON()).toEqual({
          current_password: "Admin-login-password",
          current_payment_password: "Old-funds-password",
          new_payment_password: "New-funds-password",
          code: "123456",
        });
        return route.fulfill({
          status: 403,
          contentType: "application/json",
          body: JSON.stringify({
            code: -1,
            message: "verification rejected",
            data: { error: "FUNDS_AUTHENTICATION_FAILED" },
          }),
        });
      }
      return route.fulfill(
        ok({
          configured: true,
          enforced: true,
          credential_epoch: 2,
          totp_required: true,
        }),
      );
    }
    if (p.endsWith("/security/totp"))
      return route.fulfill(ok({ enabled: true }));
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/system/change-password");
  await page.getByRole("button", { name: "修改资金密码", exact: true }).click();
  const panel = page.getByRole("region", { name: "管理员资金密码" });
  await panel
    .getByLabel("当前管理员登录密码", { exact: true })
    .fill("Admin-login-password");
  await panel
    .getByLabel("当前资金密码", { exact: true })
    .fill("Old-funds-password");
  await panel
    .getByLabel("新资金密码", { exact: true })
    .fill("New-funds-password");
  await panel
    .getByLabel("确认新资金密码", { exact: true })
    .fill("New-funds-password");
  await panel.getByLabel("当前动态验证码", { exact: true }).fill("123456");
  await panel
    .getByRole("button", { name: "确认更新资金密码", exact: true })
    .click();
  expect(writes).toBe(1);
  await expect(panel.getByRole("alert")).toContainText("verification rejected");
  for (const input of await panel.locator("input").all())
    await expect(input).toHaveValue("");
  await expect(
    page.getByRole("heading", { name: "管理员登录", exact: true }),
  ).toHaveCount(0);
  await panel
    .getByLabel("当前资金密码", { exact: true })
    .fill("Try-again-password");
  await panel
    .getByRole("button", { name: "取消资金密码修改", exact: true })
    .click();
  await expect(
    panel.getByRole("button", { name: "修改资金密码", exact: true }),
  ).toBeVisible();
  expect(writes).toBe(1);
  expect(await page.content()).not.toContain("Old-funds-password");
});
test("funds status failure blocks a new write and an initial428 is not retried with a new authorization", async ({
  page,
}) => {
  let mode = "malformed",
    writes = 0,
    challenges = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok(
          mode === "malformed"
            ? {}
            : {
                configured: false,
                enforced: false,
                credential_epoch: 0,
                totp_required: false,
              },
        ),
      );
    if (p.endsWith("/orders/ORIGINAL") && r.method() === "GET")
      return route.fulfill(
        ok({
          order: { trade_no: "ORIGINAL", status: "paid", amount: "100" },
          reporting: { refunded_amount: "0" },
          admin_control: { status: "", archived: false, revision: "0" },
        }),
      );
    if (p.endsWith("/challenges")) {
      challenges++;
      return route.fulfill(ok({}));
    }
    if (p.endsWith("/refund")) {
      writes++;
      return route.fulfill({
        status: 428,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          message: "payment authorization required; recheck original action",
        }),
      });
    }
    return route.fulfill(
      ok({
        items: [
          {
            trade_no: "ORIGINAL",
            status: "paid",
            amount: "100",
            money: "1.00",
          },
        ],
        total: 1,
      }),
    );
  });
  await page.goto("/admin/trade/order");
  await openAdminAPIRefund(page, "ORIGINAL");
  const form = page.getByRole("dialog", { name: "API退款确认", exact: true });
  await form.getByLabel("退款金额（元）", { exact: true }).fill("1.00");
  await form.getByRole("button", { name: "确认申请退款", exact: true }).click();
  await expect(form.getByRole("alert")).toContainText("资金安全状态响应不完整");
  expect(writes).toBe(0);
  mode = "off";
  await form.getByRole("button", { name: "确认申请退款", exact: true }).click();
  await expect(form.getByRole("alert")).toContainText(
    "payment authorization required",
  );
  expect(writes).toBe(1);
  expect(challenges).toBe(0);
  await expect(
    page.getByRole("dialog", { name: "管理员资金二次授权" }),
  ).toHaveCount(0);
});
