import { publicSiteFixture191 } from "./fixtures/site191";
import { test, expect } from "@playwright/test";
import currentPolicies from "./referral170-policies.json" with { type: "json" };
test.use({ trace: "off" });
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});

test("super policy writes require explicit confirmation and original CAS body while operators cannot open policy", async ({
  page,
}) => {
  let writes = 0,
    reads = 0;
  let policy: any = {
    id: "0",
    scope: "global",
    scope_id: "0",
    revision: "0",
    enabled: null,
    rate_bps: null,
    mode: "immediate",
    basis: "fee",
    refund_policy: "any_successful_refund_voids_reward",
  };
  await page.route("**/console/v1/**", (route) => {
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          enforced: false,
          configured: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/referral-policies/global/0")) {
      if (route.request().method() === "POST") {
        writes++;
        const b = route.request().postDataJSON();
        expect(b).toEqual({
          expected: "0",
          enabled: true,
          rate_bps: 5000,
          mode: "immediate",
          basis: "fee",
          refund_policy: "any_successful_refund_voids_reward",
        });
        policy = { ...policy, ...b, id: "1", revision: "1" };
      } else reads++;
      return route.fulfill(ok(policy));
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/system/referral-policies");
  await expect(page.getByText(/尚未配置本范围政策/)).toBeVisible();
  await page.getByLabel("启用状态").selectOption("enabled");
  await page.getByLabel("返现比例 BPS").fill("5000");
  await page.getByRole("button", { name: "核对政策变更", exact: true }).click();
  expect(writes).toBe(0);
  await page
    .getByRole("button", { name: "确认保存并授权", exact: true })
    .click();
  await expect(page.getByText(/当前修订 1/)).toBeVisible();
  expect(writes).toBe(1);
  expect(reads).toBe(2);
  await page.route("**/console/v1/admin/session", (route) =>
    route.fulfill(
      ok({
        user: {
          id: "2",
          name: "Operator",
          role: "admin",
          admin_class: "operator",
        },
        csrf_token: "csrf",
      }),
    ),
  );
  await page.reload();
  await expect(page.getByText("您没有访问此页面的权限")).toBeVisible();
  expect(writes).toBe(1);
  expect(reads).toBe(2);
});

test("promotion fragment is stripped before API reads and submits only explicit registration separate from admission code", async ({
  page,
}) => {
  const bearer = "rp_" + "B".repeat(43);
  let submits = 0,
    mail = 0;
  await page.route("**/console/v1/**", (route) => {
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const r = route.request(),
      p = new URL(r.url()).pathname;
    expect(
      r.url().includes(bearer) ||
        String(r.headers()["referer"]).includes(bearer),
    ).toBe(false);
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
      mail++;
      return route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          data: {
            challenge_id: "local-opaque-proof",
            expires_at: new Date(Date.now() + 600000).toISOString(),
          },
        }),
      });
    }
    if (p.endsWith("/register")) {
      submits++;
      const b = r.postDataJSON();
      expect(
        b.promotion_token === bearer &&
          b.invite_code === "ADMISSION-ONLY" &&
          b.promoter_id === undefined,
      ).toBe(true);
      return route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          data: { merchant_id: "3", username: "local-promoted" },
        }),
      });
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/merchant/dashboard#promotion_token=" + bearer);
  await expect(page.getByLabel("推广令牌")).toBeVisible();
  expect(page.url().includes("promotion_token")).toBe(false);
  expect(submits).toBe(0);
  expect(mail).toBe(0);
  await expect(page.getByLabel("推广令牌")).toHaveAttribute("type", "password");
  await page.getByLabel("推广令牌").fill("");
  await page.getByLabel("推广令牌").fill(bearer);
  await page
    .getByLabel("邮箱", { exact: true })
    .fill("local-promoted@example.test");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.getByLabel("用户名", { exact: true }).fill("local-promoted");
  await page.getByLabel("商户名称", { exact: true }).fill("local merchant");
  await page.getByLabel("邀请码", { exact: true }).fill("ADMISSION-ONLY");
  await page.getByLabel("邮箱验证码", { exact: true }).fill("1234567");
  await page
    .getByLabel("新密码", { exact: true })
    .fill("local-register-password-456");
  await page
    .getByLabel("确认新密码", { exact: true })
    .fill("local-register-password-456");
  expect(submits).toBe(0);
  await page.getByRole("button", { name: "完成注册", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("注册完成");
  expect(submits).toBe(1);
  expect(
    await page.evaluate(
      () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
    ),
  ).not.toContain(bearer);
});
test("referral merchant owns token rotation and sees historical flows rather than spendable balance", async ({
  page,
}) => {
  let version = 0,
    rotates = 0,
    currentPolicyReads = 0,
    unexpectedReads = 0;
  const bearer = "rp_" + "A".repeat(43);
  await page.route("**/console/v1/**", (route) => {
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const p = new URL(route.request().url()).pathname;
    if (p === "/console/v1/public/layout") {
      expect(route.request().method()).toBe("GET");
      expect(new URL(route.request().url()).search).toBe("");
      return route.fulfill(ok({ zhuce: "", footer: "", format: "html" }));
    }
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "local", role: "merchant" },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/referral/overview"))
      return route.fulfill(
        ok({
          invited_count: "1",
          issued_amount: "101",
          voided_amount: "50",
          recovery_pending_amount: "20",
          today_issued: "1",
          yesterday_issued: "100",
        }),
      );
    if (p.endsWith("/referral/current-policy")) {
      expect(route.request().method()).toBe("GET");
      expect(new URL(route.request().url()).search).toBe("");
      currentPolicyReads++;
      return route.fulfill(ok(currentPolicies[0]));
    }
    if (p.endsWith("/referral/token/rotate")) {
      rotates++;
      expect(route.request().postDataJSON()).toEqual({
        expected: String(version),
      });
      version++;
      return route.fulfill(
        ok({
          id: String(version),
          merchant_id: "1",
          version: String(version),
          enabled: true,
          masked: "rp_****AAAA",
          token: bearer,
          created_at: new Date().toISOString(),
          expires_at: new Date(Date.now() + 86400000).toISOString(),
        }),
      );
    }
    if (p.endsWith("/referral/token"))
      return route.fulfill(
        ok({
          id: String(version),
          merchant_id: "1",
          version: String(version),
          enabled: version > 0,
          masked: version ? "rp_****AAAA" : "",
          created_at: new Date().toISOString(),
        }),
      );
    if (p.endsWith("/referral/invitees"))
      return route.fulfill(
        ok({
          items: [
            {
              id: "3",
              invited_id: "2",
              promoter_id: "1",
              token_version: "1",
              created_at: "2026-10-23T00:00:00Z",
            },
          ],
          next_cursor: "",
        }),
      );
    if (p.endsWith("/referral/rewards"))
      return route.fulfill(
        ok({
          items: [
            {
              id: "5",
              order_id: "10",
              invited_id: "2",
              trade_no: "local-trade",
              amount: "20",
              state: "recovery_pending",
              created_at: "2026-10-23T00:00:00Z",
            },
          ],
          next_cursor: "",
        }),
      );
    if (route.request().method() === "GET") unexpectedReads++;
    return route.fulfill(ok({}));
  });
  await page.goto("/merchant/invite");
  await expect(page.getByText("累计已发")).toBeVisible();
  await expect(page.getByText("累计已撤")).toBeVisible();
  await expect(
    page.getByText("退款暂存", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText(/不代表当前可提现余额/)).toBeVisible();
  expect(unexpectedReads).toBe(0);
  expect(currentPolicyReads).toBe(1);
  await page
    .getByRole("button", { name: "发行或轮换推广令牌", exact: true })
    .click();
  expect(rotates).toBe(0);
  await page
    .getByRole("button", { name: "确认轮换并作废旧链接", exact: true })
    .click();
  expect(rotates).toBe(1);
  await expect(page.getByLabel("本次推广链接")).toHaveAttribute(
    "type",
    "password",
  );
  expect(
    await page.evaluate(
      () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
    ),
  ).not.toContain(bearer);
  await page
    .getByRole("button", { name: "重新读取邀请资料", exact: true })
    .click();
  await expect(page.getByLabel("本次推广链接")).toHaveCount(0);
  expect(rotates).toBe(1);
});
