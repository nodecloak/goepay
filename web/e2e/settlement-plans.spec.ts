import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const plan = {
  id: "4",
  revision: "1",
  name: "每日申请",
  mode: "generate_only",
  enabled: false,
  last_authorization_id: "0",
  merchant_ids: ["2"],
  schedule: {
    frequency: "day",
    interval: "1",
    anchor_at: "2026-10-24T00:00:00Z",
    execute_hour: 9,
    execute_minute: 0,
    execute_second: 0,
  },
};
test("plan generation is separate from definition and mandatory authorization while operator access is refused", async ({
  page,
}) => {
  let writes = 0,
    scans = 0;
  const paths: string[] = [];
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    paths.push(p);
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/settlement-plans")) {
      if (req.method() === "POST") writes++;
      return route.fulfill(ok({ items: [plan], next_cursor: "" }));
    }
    if (p.endsWith("/settlement-plans/4")) return route.fulfill(ok(plan));
    if (p.endsWith("/settlement-plans/4/runs")) {
      if (req.method() === "POST") {
        scans++;
        expect(req.postDataJSON()).toEqual({ expected_revision: "1" });
        return route.fulfill(
          ok(
            {
              id: "9",
              plan_id: "4",
              state: "blocked",
              created_count: "0",
              skipped_count: "0",
              blocked_count: "1",
              error_count: "0",
              items: [],
            },
            202,
          ),
        );
      }
      return route.fulfill(ok({ items: [], next_cursor: "" }));
    }
    if (p.endsWith("/authorizations")) {
      writes++;
      return route.fulfill(ok({}));
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/settle/plans");
  await expect(page.getByRole("button", { name: "查看计划 4" })).toBeVisible();
  await expect(page.getByText(/仅生成待审核申请并冻结/).first()).toBeVisible();
  await page.getByRole("button", { name: "查看计划 4" }).click();
  await page.getByLabel("授权开始 RFC3339").fill("2026-10-24T00:00:00Z");
  await page.getByLabel("授权截止 RFC3339").fill("2026-10-25T00:00:00Z");
  await page.getByLabel("单项毛额上限（分）").fill("100");
  await page.getByLabel("每次毛额上限（分）").fill("200");
  await page.getByLabel("每日毛额上限（分）").fill("300");
  await page.getByLabel("每日条数上限").fill("2");
  await page.getByRole("button", { name: "核对计划授权" }).click();
  expect(writes).toBe(0);
  await page.getByRole("button", { name: "确认授权并验证资金密码" }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: /尚未设置本人管理员资金密码/ }),
  ).toBeVisible();
  expect(writes).toBe(0);
  await page.getByRole("button", { name: "重新读取计划" }).click();
  await page.getByRole("button", { name: "核对到期扫描" }).click();
  expect(scans).toBe(0);
  await page.getByRole("button", { name: "确认扫描当前到期时段" }).click();
  await expect(page.getByText(/扫描已受理/)).toBeVisible();
  expect(scans).toBe(1);
  expect(paths.some((p) => /payout.*execute|transfers/.test(p))).toBe(false);
  await page.route("**/console/v1/admin/session", (r) =>
    r.fulfill(
      ok({
        user: { id: "3", name: "Op", role: "admin", admin_class: "operator" },
        csrf_token: "c",
      }),
    ),
  );
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "您没有访问此页面的权限" }),
  ).toBeVisible();
});

test("merchant explicitly opts in to current masked recipient configuration without making a settlement request", async ({
  page,
}) => {
  let writes = 0;
  let prefs = {
    merchant_id: "2",
    revision: "0",
    enabled: false,
    recipient_config_id: "0",
    recipient: {
      state: "configured",
      config_id: "12",
      type: "alipay",
      app_id: "",
      account_masked: "***@example.test",
      name_masked: "***",
    },
  };
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({ user: { id: "2", name: "M", role: "merchant" }, csrf_token: "c" }),
      );
    if (p.endsWith("/account/settlement-schedule")) {
      if (req.method() === "PUT") {
        writes++;
        expect(req.postDataJSON()).toEqual({
          expected_revision: "0",
          enabled: true,
          recipient_config_id: "12",
        });
        prefs = { ...prefs, enabled: true, revision: "1" };
      }
      return route.fulfill(ok(prefs));
    }
    return route.fulfill(ok({}));
  });
  await page.setViewportSize({ width: 375, height: 850 });
  await page.goto("/merchant/account/settlement-schedule");
  await expect(page.getByText("***@example.test")).toBeVisible();
  expect(writes).toBe(0);
  await page.getByLabel("同意参与计划生成").check();
  await page.getByRole("button", { name: "核对参与设置" }).click();
  expect(writes).toBe(0);
  await page.getByRole("button", { name: "确认保存参与设置" }).click();
  await expect(page.getByText(/参与设置已保存/)).toBeVisible();
  expect(writes).toBe(1);
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
});

test("plan authorization always obtains an exact-body grant even when global enforcement is disabled", async ({
  page,
}) => {
  let challenged: any,
    submitted = 0;
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "A", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: true,
          enforced: false,
          credential_epoch: 1,
          totp_required: false,
        }),
      );
    if (p.endsWith("/settlement-plans"))
      return route.fulfill(ok({ items: [plan], next_cursor: "" }));
    if (p.endsWith("/settlement-plans/4")) return route.fulfill(ok(plan));
    if (p.endsWith("/runs"))
      return route.fulfill(ok({ items: [], next_cursor: "" }));
    if (p.endsWith("/funds-authorizations/challenges")) {
      challenged = req.postDataJSON();
      expect(challenged.target).toBe(
        "/console/v1/admin/settlement-plans/4/authorizations",
      );
      return route.fulfill(
        ok({
          challenge_id: "local-plan-challenge",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: {
            mode: "generate_only",
            max_item_gross: "100",
            recipient: "masked configs",
          },
        }),
      );
    }
    if (p.endsWith("/funds-authorizations/confirm")) {
      expect(req.postDataJSON()).toEqual({
        challenge_id: "local-plan-challenge",
        payment_password: "Local-plan-password",
      });
      return route.fulfill(
        ok({
          authorization: "local-plan-grant",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    }
    if (p.endsWith("/settlement-plans/4/authorizations")) {
      submitted++;
      expect(req.postDataJSON()).toEqual(challenged.body);
      expect(req.headers()["x-payment-authorization"]).toBe("local-plan-grant");
      return route.fulfill(
        ok({ id: "8", plan_id: "4", plan_revision: "1" }, 201),
      );
    }
    return route.fulfill(ok({}));
  });
  await page.goto("/admin/settle/plans");
  await page.getByRole("button", { name: "查看计划 4" }).click();
  for (const [label, value] of [
    ["授权开始 RFC3339", "2026-10-24T00:00:00Z"],
    ["授权截止 RFC3339", "2026-10-25T00:00:00Z"],
    ["单项毛额上限（分）", "100"],
    ["每次毛额上限（分）", "200"],
    ["每日毛额上限（分）", "300"],
    ["每日条数上限", "2"],
  ])
    await page.getByLabel(label!).fill(value!);
  await page.getByRole("button", { name: "核对计划授权" }).click();
  await page.getByRole("button", { name: "确认授权并验证资金密码" }).click();
  const modal = page.getByRole("dialog", { name: "管理员资金二次授权" });
  await expect(modal).toContainText("masked configs");
  expect(submitted).toBe(0);
  expect(challenged.body).toEqual({
    expected_revision: "1",
    expected_authorization_id: "0",
    valid_from: "2026-10-24T00:00:00Z",
    expires_at: "2026-10-25T00:00:00Z",
    max_item_gross: "100",
    max_run_gross: "200",
    max_day_gross: "300",
    max_day_items: "2",
  });
  await modal
    .getByRole("button", { name: "已核对摘要，验证资金密码", exact: true })
    .click();
  await modal
    .getByLabel("本人管理员资金密码", { exact: true })
    .fill("Local-plan-password");
  await modal
    .getByRole("button", { name: "确认资金密码", exact: true })
    .click();
  expect(submitted).toBe(0);
  await modal
    .getByRole("button", { name: "执行已核对的原操作", exact: true })
    .click();
  await expect(page.getByText(/原计划授权已保存/)).toBeVisible();
  expect(submitted).toBe(1);
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
});
