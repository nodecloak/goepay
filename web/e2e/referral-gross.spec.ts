import { test, expect, type Page } from "@playwright/test";

const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const policy = {
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
async function localOnly(page: Page, baseURL: string | undefined) {
  if (!baseURL || !/^http:\/\/127\.0\.0\.1:\d+$/.test(new URL(baseURL).origin))
    throw Error("Loopback mock fixture required");
  const origin = new URL(baseURL).origin,
    foreign: string[] = [];
  await page.context().route("**/*", (route) => {
    if (new URL(route.request().url()).origin === origin)
      return route.continue();
    foreign.push(new URL(route.request().url()).origin);
    return route.abort();
  });
  return foreign;
}

test("explicit gross policy preserves its original CAS basis through funds confirmation", async ({
  page,
  baseURL,
}) => {
  const foreign = await localOnly(page, baseURL);
  let current = { ...policy },
    frozen: Record<string, unknown> | undefined;
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
          csrf_token: "synthetic-gross-csrf",
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
      expect(body.target).toBe("/console/v1/admin/referral-policies/global/0");
      frozen = body.body;
      expect(frozen).toEqual({
        expected: "0",
        enabled: true,
        rate_bps: 5000,
        mode: "immediate",
        basis: "gross",
        refund_policy: "any_successful_refund_voids_reward",
      });
      return route.fulfill(
        ok({
          challenge_id: "synthetic-gross-challenge",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: {
            basis: "gross",
            reward_basis:
              "原订单金额比例，按分四舍五入，奖励不超过原订单平台手续费",
            reward_budget: "仅原订单平台手续费；不使用其它订单资金",
          },
        }),
      );
    }
    if (path.endsWith("/funds-authorizations/confirm")) {
      expect(r.postDataJSON()).toEqual({
        challenge_id: "synthetic-gross-challenge",
        payment_password: "synthetic-funds-password",
      });
      return route.fulfill(
        ok({
          authorization: "synthetic-once-gross-grant",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    }
    if (path.endsWith("/referral-policies/global/0")) {
      if (r.method() === "POST") {
        posted.push(r.postDataJSON());
        expect(r.postDataJSON()).toEqual(frozen);
        expect(r.headers()["x-payment-authorization"]).toBe(
          "synthetic-once-gross-grant",
        );
        expect(r.headers()["x-csrf-token"]).toBe("synthetic-gross-csrf");
        current = { ...current, ...r.postDataJSON(), id: "1", revision: "1" };
      }
      return route.fulfill(ok(current));
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/system/referral-policies");
  await expect(
    page.getByRole("combobox", { name: "奖励基数", exact: true }),
  ).toHaveValue("fee");
  await page
    .getByRole("combobox", { name: "启用状态", exact: true })
    .selectOption("enabled");
  await page.getByLabel("返现比例 BPS", { exact: true }).fill("5000");
  await page
    .getByRole("combobox", { name: "奖励基数", exact: true })
    .selectOption("gross");
  await page.getByRole("button", { name: "核对政策变更", exact: true }).click();
  await expect(page.getByText(/确认全局政策修订/)).toContainText("原订单金额");
  expect(posted).toEqual([]);
  await page
    .getByRole("button", { name: "确认保存并授权", exact: true })
    .click();
  const modal = page.getByRole("dialog", {
    name: "管理员资金二次授权",
    exact: true,
  });
  await expect(modal).toContainText("奖励不超过原订单平台手续费");
  expect(posted).toEqual([]);
  await modal
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await modal
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("synthetic-funds-password");
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
  await expect(page.getByText(/当前修订 1/)).toBeVisible();
  await expect(
    page.getByRole("combobox", { name: "奖励基数", exact: true }),
  ).toHaveValue("gross");
  expect(posted).toEqual([frozen]);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.screenshot({
    path: "../.tools/referral-gross69-ui-375.png",
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(
    await page.evaluate(
      () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
    ),
  ).not.toContain("synthetic-once-gross-grant");
  expect(page.url()).not.toContain("grant");
  expect(foreign).toEqual([]);
});

test("group basis overrides explicitly and an uncertain save never retries without a new review", async ({
  page,
  baseURL,
}) => {
  const foreign = await localOnly(page, baseURL);
  let writes = 0,
    reads = 0;
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
          csrf_token: "synthetic-gross-csrf",
        }),
      );
    if (path.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (path.includes("/referral-policies/")) {
      if (r.method() === "POST") {
        writes++;
        expect(path).toBe("/console/v1/admin/referral-policies/group/9");
        expect(r.postDataJSON()).toEqual({
          expected: "0",
          enabled: null,
          rate_bps: null,
          mode: "immediate",
          basis: "fee",
          refund_policy: "any_successful_refund_voids_reward",
        });
        return route.abort("failed");
      }
      reads++;
      return route.fulfill(
        ok(
          path.endsWith("/group/9")
            ? { ...policy, scope: "group", scope_id: "9", basis: "gross" }
            : policy,
        ),
      );
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/system/referral-policies");
  await expect(
    page.getByRole("combobox", { name: "奖励基数", exact: true }),
  ).toHaveValue("fee");
  await page
    .getByRole("combobox", { name: "政策范围", exact: true })
    .selectOption("group");
  await page.getByLabel("用户组 ID", { exact: true }).fill("9");
  await page.getByRole("button", { name: "读取政策版本", exact: true }).click();
  await expect(
    page.getByRole("combobox", { name: "奖励基数", exact: true }),
  ).toHaveValue("gross");
  await expect(page.getByText(/存在用户组政策时/)).toContainText(
    "不继承上层基数",
  );
  await page
    .getByRole("combobox", { name: "奖励基数", exact: true })
    .selectOption("fee");
  await page.getByRole("button", { name: "核对政策变更", exact: true }).click();
  await page
    .getByRole("button", { name: "确认保存并授权", exact: true })
    .click();
  await expect(page.getByText(/不会自动重复提交/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "核对政策变更", exact: true }),
  ).toBeDisabled();
  expect(writes).toBe(1);
  expect(reads).toBe(2);
  await page.getByRole("button", { name: "读取政策版本", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "核对政策变更", exact: true }),
  ).toBeEnabled();
  expect(writes).toBe(1);
  expect(reads).toBe(3);
  expect(foreign).toEqual([]);
});
