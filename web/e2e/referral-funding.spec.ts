import { test, expect, type Page } from "@playwright/test";
const ok = (data: unknown) => ({
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const ordinary = {
  id: "0",
  scope: "global",
  scope_id: "0",
  revision: "0",
  enabled: false,
  rate_bps: 0,
  mode: "immediate",
  basis: "fee",
  refund_policy: "any_successful_refund_voids_reward",
  daily_mode_available: true,
  profit_basis_available: true,
};
async function localOnly(page: Page, baseURL?: string) {
  if (!baseURL || !/^http:\/\/127\.0\.0\.1:\d+$/.test(new URL(baseURL).origin))
    throw Error("Loopback-only synthetic fixture required");
  const origin = new URL(baseURL).origin,
    foreign: string[] = [];
  await page.context().route("**/*", (r) => {
    if (new URL(r.request().url()).origin === origin) return r.continue();
    foreign.push(new URL(r.request().url()).origin);
    return r.abort();
  });
  return foreign;
}
for (const family of [
  "membership",
  "profit-immediate",
  "profit-daily",
] as const)
  test(`${family} keeps its own immutable policy body through the administrator funds confirmation`, async ({
    page,
    baseURL,
  }) => {
    const foreign = await localOnly(page, baseURL),
      membership = family === "membership";
    const endpoint = membership
      ? "/membership-referral-policies/global/0"
      : "/referral-policies/global/0";
    let current: Record<string, unknown> = membership
      ? {
          available: true,
          id: "0",
          scope: "global",
          scope_id: "0",
          revision: "0",
          enabled: false,
          rate_bps: 0,
        }
      : { ...ordinary };
    let frozen: Record<string, unknown> | undefined;
    const posted: unknown[] = [];
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        path = new URL(r.url()).pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Local super",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "synthetic-funding-csrf",
          }),
        );
      if (path.endsWith("/security/payment-password"))
        return route.fulfill(
          ok({
            configured: true,
            enforced: true,
            credential_epoch: 1,
            totp_required: false,
          }),
        );
      if (path.endsWith("/funds-authorizations/challenges")) {
        const body = r.postDataJSON();
        expect(body.target).toBe("/console/v1/admin" + endpoint);
        frozen = body.body;
        expect(frozen).toEqual(
          membership
            ? { expected: "0", enabled: true, rate_bps: 2500 }
            : {
                expected: "0",
                enabled: true,
                rate_bps: 2500,
                mode: family === "profit-daily" ? "daily" : "immediate",
                basis: "profit",
                refund_policy: "any_successful_refund_voids_reward",
              },
        );
        return route.fulfill(
          ok({
            challenge_id: "synthetic-funding-challenge",
            expires_at: new Date(Date.now() + 120000).toISOString(),
            summary: {
              reward_budget: membership
                ? "仅该次真实余额会员购买收入"
                : "仅原订单手续费",
              reward_basis: membership
                ? "原购买价格乘比例"
                : "配置估计利润，非实际账单",
            },
          }),
        );
      }
      if (path.endsWith("/funds-authorizations/confirm")) {
        expect(r.postDataJSON()).toEqual({
          challenge_id: "synthetic-funding-challenge",
          payment_password: "synthetic-funding-password",
        });
        return route.fulfill(
          ok({
            authorization: "synthetic-funding-once",
            expires_at: new Date(Date.now() + 120000).toISOString(),
          }),
        );
      }
      if (path === "/console/v1/admin" + endpoint) {
        if (r.method() === "POST") {
          expect(r.postDataJSON()).toEqual(frozen);
          expect(r.headers()["x-payment-authorization"]).toBe(
            "synthetic-funding-once",
          );
          expect(r.headers()["x-csrf-token"]).toBe("synthetic-funding-csrf");
          posted.push(r.postDataJSON());
          current = { ...current, ...r.postDataJSON(), id: "1", revision: "1" };
        }
        return route.fulfill(ok(current));
      }
      if (path.endsWith("/referral-policies/global/0"))
        return route.fulfill(
          ok({ ...ordinary, membership_referral_policy_available: true }),
        );
      return route.fulfill(ok({}));
    });
    await page.goto("/admin/system/referral-policies");
    const panel = page.getByRole("region", {
      name: membership ? "会员推广奖励政策" : "订单返现政策",
      exact: true,
    });
    await expect(panel).toBeVisible();
    if (membership) {
      await panel
        .getByRole("combobox", { name: "启用状态", exact: true })
        .selectOption("enabled");
    } else {
      // 启用状态 is now a KitSelect (custom listbox): open the trigger, pick 启用.
      await panel
        .getByRole("button", { name: "启用状态", exact: true })
        .click();
      await panel.getByRole("option", { name: "启用", exact: true }).click();
    }
    await panel
      .getByLabel(membership ? "会员奖励比例 BPS" : "返现比例 BPS", {
        exact: true,
      })
      .fill("2500");
    if (!membership) {
      // 奖励基数 and 发奖时机 are KitSelect too.
      await panel
        .getByRole("button", { name: "奖励基数", exact: true })
        .click();
      await panel
        .getByRole("option", {
          name: "配置估计利润（非实际账单）",
          exact: true,
        })
        .click();
      await panel
        .getByRole("button", { name: "发奖时机", exact: true })
        .click();
      await panel
        .getByRole("option", {
          name: family === "profit-daily" ? "次日北京时间待发" : "即时发奖",
          exact: true,
        })
        .click();
    }
    await panel
      .getByRole("button", {
        name: membership ? "核对会员奖励政策" : "核对政策变更",
        exact: true,
      })
      .click();
    expect(posted).toEqual([]);
    await panel
      .getByRole("button", { name: "确认保存并授权", exact: true })
      .click();
    const modal = page.getByRole("dialog", {
      name: "管理员资金二次授权",
      exact: true,
    });
    await expect(modal).toContainText(
      membership ? "仅该次真实余额会员购买收入" : "配置估计利润，非实际账单",
    );
    await modal
      .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
      .click();
    await modal
      .getByLabel("本人管理员资金密码", { exact: true })
      .fill("synthetic-funding-password");
    await modal
      .getByRole("button", { name: "确认资金密码", exact: true })
      .click();
    await expect(
      modal.getByRole("button", { name: "执行已核对的原操作", exact: true }),
    ).toBeVisible();
    expect(posted).toEqual([]);
    await modal
      .getByRole("button", { name: "执行已核对的原操作", exact: true })
      .click();
    await expect(panel).toContainText("当前修订 1");
    expect(posted).toEqual([frozen]);
    await page.setViewportSize({ width: 375, height: 812 });
    await expect
      .poll(async () => {
        const b = await page.locator(".console-sidebar").boundingBox();
        return b ? b.x + b.width : Infinity;
      })
      .toBeLessThanOrEqual(1);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.screenshot({
      path: `test-results/${family}-375.png`,
      fullPage: true,
      animations: "disabled",
    });
    expect(
      await page.evaluate(
        () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
      ),
    ).not.toContain("synthetic-funding-once");
    expect(foreign).toEqual([]);
  });
