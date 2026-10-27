import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";
import { publicSiteFixture191 } from "./fixtures/site191";
import { test, expect } from "@playwright/test";
import actualPlatform from "./globalaccess179-http-fixture.json" with { type: "json" };
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("mail-disabled enrollment cannot send and leaving the form clears sensitive component state", async ({
  page,
}) => {
  let sends = 0;
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p === "/console/v1/public/merchant-access")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      });
    if (p === "/console/v1/public/layout")
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (p.endsWith("/session"))
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ code: -1, message: "sign in" }),
      });
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          registration_open: true,
          email_enabled: false,
          invite_required: false,
          expires_seconds: 600,
        }),
      );
    sends++;
    return route.fulfill(ok(null));
  });
  await page.goto("/merchant/dashboard");
  await page.getByRole("button", { name: "注册商户", exact: true }).click();
  await expect(
    page.getByText("邮件发送尚未配置，当前不能发送验证码。"),
  ).toBeVisible();
  await page.getByLabel("邮箱", { exact: true }).fill("unbound@example.test");
  await page
    .getByLabel("新密码", { exact: true })
    .fill("Temporary-password-123");
  await expect(
    page.getByRole("button", { name: "发送验证码", exact: true }),
  ).toBeDisabled();
  expect(sends).toBe(0);
  await page.getByRole("button", { name: "返回登录", exact: true }).click();
  await page.getByRole("button", { name: "注册商户", exact: true }).click();
  await expect(page.getByLabel("新密码", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("邮箱", { exact: true })).toHaveValue("");
  expect(
    await page.evaluate(() =>
      Object.keys(localStorage).concat(Object.keys(sessionStorage)),
    ),
  ).toEqual([]);
});
test("registration posts opaque challenge with invitation, handles wrong code and never logs in automatically", async ({
  page,
}) => {
  const bodies: Record<string, unknown>[] = [];
  let submits = 0,
    logins = 0;
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p === "/console/v1/public/merchant-access")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      });
    if (p === "/console/v1/public/layout")
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (p.endsWith("/session"))
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ code: -1 }),
      });
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          registration_open: true,
          email_enabled: true,
          invite_required: true,
          expires_seconds: 600,
        }),
      );
    if (p.endsWith("/verification-code")) {
      expect(r.postDataJSON()).toEqual({
        purpose: "register",
        email: "new@example.test",
      });
      expect(r.headers()["content-type"]).toBe("application/json");
      return route.fulfill(
        ok(
          {
            challenge_id: "opaque-challenge",
            expires_at: new Date(Date.now() + 600000).toISOString(),
          },
          202,
        ),
      );
    }
    if (p.endsWith("/register")) {
      bodies.push(r.postDataJSON());
      submits++;
      if (submits === 1)
        return route.fulfill({
          status: 422,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "invalid code" }),
        });
      return route.fulfill(
        ok({ merchant_id: "2001", username: "new-merchant" }, 201),
      );
    }
    if (p.endsWith("/login")) logins++;
    return route.fulfill(ok(null));
  });
  await page.goto("/merchant/dashboard");
  await page.getByRole("button", { name: "注册商户", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("New@Example.Test");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /秒后可重新发送/ }),
  ).toBeDisabled();
  await page.getByLabel("用户名", { exact: true }).fill("new-merchant");
  await page.getByLabel("商户名称", { exact: true }).fill("Test Merchant");
  await page
    .getByLabel("新密码", { exact: true })
    .fill("Temporary-password-123");
  await page
    .getByLabel("确认新密码", { exact: true })
    .fill("Temporary-password-123");
  await page.getByLabel("邀请码", { exact: true }).fill("INVITE");
  await page.getByLabel("邮箱验证码", { exact: true }).fill("1234567");
  await page.getByRole("button", { name: "完成注册", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("验证码");
  await page.getByLabel("邮箱验证码", { exact: true }).fill("7654321");
  await page.getByRole("button", { name: "完成注册", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("注册完成");
  expect(bodies[1]).toEqual({
    email: "new@example.test",
    challenge_id: "opaque-challenge",
    code: "7654321",
    username: "new-merchant",
    name: "Test Merchant",
    password: "Temporary-password-123",
    invite_code: "INVITE",
  });
  expect(logins).toBe(0);
  await expect(page.getByLabel("新密码", { exact: true })).toHaveCount(0);
});
test("recovery invalidates a challenge after editing email and reports uniform completion without disabling MFA", async ({
  page,
}) => {
  let recovery: unknown;
  let sends = 0;
  await page.clock.install();
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p === "/console/v1/public/merchant-access")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      });
    if (p === "/console/v1/public/layout")
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (p.endsWith("/session"))
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ code: -1 }),
      });
    if (p.endsWith("/capabilities"))
      return route.fulfill(
        ok({
          registration_open: false,
          email_enabled: true,
          invite_required: false,
          expires_seconds: 600,
        }),
      );
    if (p.endsWith("/verification-code")) {
      sends++;
      return route.fulfill(
        ok(
          {
            challenge_id: `recovery-challenge-${sends}`,
            expires_at: new Date(Date.now() + 600000).toISOString(),
          },
          202,
        ),
      );
    }
    if (p.endsWith("/recover-password")) {
      recovery = r.postDataJSON();
      return route.fulfill(ok(null));
    }
    return route.fulfill(ok(null));
  });
  await page.goto("/merchant/dashboard");
  await page.getByRole("button", { name: "找回密码", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("bound@example.test");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("other@example.test");
  await page.getByLabel("邮箱", { exact: true }).fill("bound@example.test");
  await expect(
    page.getByRole("button", { name: "提交密码恢复", exact: true }),
  ).toBeDisabled();
  await page.clock.fastForward(60001);
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.getByLabel("新密码", { exact: true }).fill("New-password-123");
  await page.getByLabel("确认新密码", { exact: true }).fill("New-password-123");
  await page.getByLabel("邮箱验证码", { exact: true }).fill("1234567");
  await page.getByRole("button", { name: "提交密码恢复", exact: true }).click();
  expect(recovery).toEqual({
    email: "bound@example.test",
    challenge_id: "recovery-challenge-2",
    code: "1234567",
    new_password: "New-password-123",
  });
  await expect(page.getByRole("status")).toContainText("请求处理完成");
  await expect(page.getByRole("status")).toContainText("动态验证码");
});
test("unknown capabilities fail closed and mail service/rate-limit failures never create a usable challenge", async ({
  page,
}) => {
  let capabilities = 0,
    sends = 0;
  await page.route("**/console/v1/**", (route) => {
    const captcha205Reply = closedLoginCaptchaFixture205(route);
    if (captcha205Reply) return captcha205Reply;
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p === "/console/v1/public/merchant-access")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      });
    if (p === "/console/v1/public/layout")
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    if (p.endsWith("/session"))
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ code: -1 }),
      });
    if (p.endsWith("/capabilities")) {
      capabilities++;
      return route.fulfill(
        ok(
          capabilities === 1
            ? { registration_open: true }
            : {
                registration_open: false,
                email_enabled: true,
                invite_required: false,
                expires_seconds: 600,
              },
        ),
      );
    }
    sends++;
    return route.fulfill({
      status: sends === 1 ? 503 : 429,
      contentType: "application/json",
      body: JSON.stringify({ code: -1, message: "unavailable" }),
    });
  });
  await page.goto("/merchant/dashboard");
  await page.getByRole("button", { name: "注册商户", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "发送验证码", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "重新读取服务能力", exact: true })
    .click();
  await expect(page.getByText("当前未开放商户注册。")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "发送验证码", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "返回登录", exact: true }).click();
  await page.getByRole("button", { name: "找回密码", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("bound@example.test");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("未确认发送");
  await expect(
    page.getByRole("button", { name: "提交密码恢复", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("未提供准确恢复时间");
  await expect(
    page.getByRole("button", { name: "提交密码恢复", exact: true }),
  ).toBeDisabled();
});
