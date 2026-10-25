import { test, expect, type Page } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
async function localOnly(page: Page, baseURL: string | undefined) {
  if (!baseURL || !/^http:\/\/127\.0\.0\.1:\d+$/.test(new URL(baseURL).origin))
    throw Error("Loopback mock only");
  const origin = new URL(baseURL).origin,
    foreign: string[] = [];
  await page.context().route("**/*", (route) => {
    if (new URL(route.request().url()).origin === origin)
      return route.continue();
    foreign.push(route.request().url());
    return route.abort();
  });
  return foreign;
}
const policy = {
  id: "1",
  scope: "global",
  scope_id: "0",
  revision: "7",
  enabled: true,
  rate_bps: 500,
  mode: "immediate",
  basis: "gross",
  refund_policy: "any_successful_refund_voids_reward",
  daily_mode_available: true,
};
test("daily selection stays in the original reviewed CAS and one-time funds grant", async ({
  page,
  baseURL,
}) => {
  const foreign = await localOnly(page, baseURL);
  let current = { ...policy },
    frozen: unknown;
  const posted: unknown[] = [];
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "Synthetic super",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "daily-csrf",
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
        "/console/v1/admin/referral-policies/global/0",
      );
      frozen = r.postDataJSON().body;
      expect(frozen).toEqual({
        expected: "7",
        enabled: true,
        rate_bps: 500,
        mode: "daily",
        basis: "gross",
        refund_policy: "any_successful_refund_voids_reward",
      });
      return route.fulfill(
        ok({
          challenge_id: "daily-challenge",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: {
            mode: "daily",
            reward_basis: "原订单金额比例，奖励不超过原订单平台手续费",
            reward_budget: "原单固定奖励，次日北京时间等待专用调度器",
          },
        }),
      );
    }
    if (p.endsWith("/funds-authorizations/confirm"))
      return route.fulfill(
        ok({
          authorization: "synthetic-once-daily-grant",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    if (p.endsWith("/referral-policies/global/0")) {
      if (r.method() === "POST") {
        expect(r.postDataJSON()).toEqual(frozen);
        expect(r.headers()["x-payment-authorization"]).toBe(
          "synthetic-once-daily-grant",
        );
        posted.push(r.postDataJSON());
        current = { ...current, ...r.postDataJSON(), revision: "8" };
      }
      return route.fulfill(ok(current));
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/system/referral-policies");
  const mode = page.getByRole("combobox", { name: "发奖时机", exact: true });
  await expect(mode).toHaveValue("immediate");
  await mode.selectOption("daily");
  await expect(page.getByText(/每笔原订单固定奖励/)).toContainText(
    "不代表调度器已启动",
  );
  await page.getByRole("button", { name: "核对政策变更", exact: true }).click();
  expect(posted).toEqual([]);
  await page
    .getByRole("button", { name: "确认保存并授权", exact: true })
    .click();
  const modal = page.getByRole("dialog", {
    name: "管理员资金二次授权",
    exact: true,
  });
  await expect(modal).toContainText("次日北京时间");
  await modal
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await modal
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("synthetic-daily-funds-password");
  await modal
    .getByRole("button", { name: "确认资金密码", exact: true })
    .click();
  expect(posted).toEqual([]);
  await modal
    .getByRole("button", { name: "执行已核对的原操作", exact: true })
    .click();
  await expect(page.getByText(/当前修订 8/)).toBeVisible();
  await expect(mode).toHaveValue("daily");
  expect(posted).toEqual([frozen]);
  expect(
    await page.evaluate(
      () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
    ),
  ).not.toContain("synthetic-once-daily-grant");
  expect(foreign).toEqual([]);
});
test("unavailable daily read cannot be edited or silently downgraded", async ({
  page,
  baseURL,
}) => {
  const foreign = await localOnly(page, baseURL);
  let writes = 0;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "Synthetic super",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "daily-csrf",
        }),
      );
    if (p.includes("/referral-policies/")) {
      if (r.method() !== "GET") writes++;
      return route.fulfill(
        ok({ ...policy, mode: "daily", daily_mode_available: false }),
      );
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/system/referral-policies");
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "核对政策变更", exact: true }),
  ).toHaveCount(0);
  expect(writes).toBe(0);
  expect(foreign).toEqual([]);
});
const row = {
  order_id: "9007199254740993",
  merchant_id: "8",
  promoter_id: "1000",
  trade_no: "DAILY-ORIGINAL-ORDER",
  amount: "9007199254740993",
  platform_fee: "1001",
  reward_amount: "501",
  pending_amount: "501",
  state: "pending",
  activated_at: "2026-10-23T16:15:00Z",
  business_day: "2026-10-24",
  eligible_day: "2026-10-25",
  source_version: 2,
  source_verified: true,
};
for (const capability of [false, true])
  test(`daily queue capability ${capability} preserves original owner and calendar with no money actions`, async ({
    page,
    baseURL,
  }) => {
    const foreign = await localOnly(page, baseURL),
      reads: string[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        url = new URL(r.url()),
        p = url.pathname;
      if (r.method() !== "GET") writes.push(p);
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", name: "Synthetic merchant", role: "merchant" },
            csrf_token: "daily-merchant-csrf",
          }),
        );
      if (p.endsWith("/referral/overview"))
        return route.fulfill(
          ok({
            invited_count: "1",
            issued_amount: "0",
            voided_amount: "0",
            recovery_pending_amount: "0",
            today_issued: "0",
            yesterday_issued: "0",
            daily_queue_available: capability,
          }),
        );
      if (p.endsWith("/referral/token"))
        return route.fulfill(
          ok({
            id: "1",
            merchant_id: "1000",
            version: "1",
            enabled: false,
            masked: "***",
          }),
        );
      if (p.endsWith("/referral/daily-queue")) {
        reads.push(url.search);
        return route.fulfill(
          ok(
            url.searchParams.get("after_id") === "0"
              ? { items: [row], next_cursor: row.order_id }
              : { items: [], next_cursor: "" },
          ),
        );
      }
      return route.fulfill(ok({ items: [], next_cursor: "" }));
    });
    await page.goto("/merchant/invite");
    await expect(
      page.getByRole("heading", { name: "邀请奖励流水", exact: true }),
    ).toBeVisible();
    const queue = page.getByRole("region", {
      name: "每日待发奖励",
      exact: true,
    });
    if (!capability) {
      await expect(queue).toHaveCount(0);
      expect(reads).toEqual([]);
    } else {
      await expect(queue).toContainText("90071992547409.93");
      await expect(queue).toContainText("原条款已验证");
      await expect(queue).toContainText("2026-10-24 00:15:00");
      await page.setViewportSize({ width: 375, height: 812 });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: "../.tools/referral-daily-queue-375.png",
        fullPage: true,
        animations: "disabled",
      });
      await queue
        .getByRole("button", { name: "下一页待发奖励", exact: true })
        .click();
      await expect(queue).toContainText("暂无每日待发奖励记录");
      expect(reads).toEqual([
        "?after_id=0&limit=20",
        "?after_id=9007199254740993&limit=20",
      ]);
    }
    expect(writes).toEqual([]);
    expect(foreign).toEqual([]);
  });
