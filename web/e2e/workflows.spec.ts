import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("settlement approvals stop at first failure and report successful subset", async ({
  page,
}) => {
  const reviewed: string[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    if (path.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "test",
        }),
      );
    if (req.method() === "POST") {
      const id = path.split("/").at(-2)!;
      reviewed.push(id);
      return id === "2"
        ? route.fulfill({
            status: 409,
            contentType: "application/json",
            body: JSON.stringify({ code: 409, message: "申请状态已变更" }),
          })
        : route.fulfill(ok({}));
    }
    return route.fulfill(
      ok({
        items: [1, 2, 3].map((id) => ({
          id: String(id),
          external_no: "request" + id,
          merchant_id: "1000",
          amount: "0.01",
          status: "pending",
        })),
        total: 3,
      }),
    );
  });
  await page.goto("/admin/settle/list");
  for (const id of ["1", "2", "3"])
    await page
      .getByRole("checkbox", { name: "选择申请 " + id, exact: true })
      .check();
  await expect(
    page.getByRole("link", { name: "将所选 3 条申请生成结算批次" }),
  ).toHaveAttribute("href", "/admin/settle/batch?request_ids=1%2C2%2C3");
  await page.getByRole("button", { name: "核准所选 3 条申请" }).click();
  expect(reviewed).toEqual([]);
  await page.getByRole("button", { name: "确认核准", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "已处理 1 条，后续操作停止",
  );
  expect(reviewed).toEqual(["1", "2"]);
});
test("internal transfer sends exact money string and preserves idempotency number", async ({
  page,
}) => {
  let body: Record<string, string> | undefined;
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    if (path.endsWith("/funds-availability"))
      return route.fulfill(
        ok({
          settle_type: 0,
          available_money: "1.00",
          ledger_available_money: "1.00",
          today_blocked_money: "0.00",
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
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "商户", role: "merchant" },
          csrf_token: "token",
        }),
      );
    if (req.method() === "POST") {
      body = req.postDataJSON();
      expect(req.headers()["x-csrf-token"]).toBe("token");
      return route.fulfill(ok({ id: "1" }));
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/account/internal-transfer");
  await page.getByRole("button", { name: "发起站内转账", exact: true }).click();
  await page.getByLabel("收款商户 ID").fill("1001");
  await page.getByLabel("金额（元）").fill("0.01");
  await page.getByLabel("业务请求号").fill("stable-test-request");
  await page.getByRole("button", { name: "确认提交", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText("申请已提交");
  expect(body).toEqual({
    recipient_id: "1001",
    amount: "0.01",
    external_no: "stable-test-request",
  });
});
test("reports send date interval and export actual response fields safely", async ({
  page,
}) => {
  const queries: string[] = [];
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "token",
        }),
      );
    queries.push(url.search);
    return route.fulfill(
      ok({
        columns: [
          { key: "day", label: "日期" },
          { key: "paid_money", label: "实收金额（元）" },
        ],
        items: [{ day: "2026-10-23", paid_money: "0.01" }],
        total: 1,
      }),
    );
  });
  await page.goto("/admin/trade/stat/daily");
  await page.getByLabel("开始日期").fill("2026-10-22");
  await page.getByLabel("结束日期").fill("2026-10-23");
  await page.getByRole("button", { name: "查询统计" }).click();
  await expect(
    page.getByRole("cell", { name: "0.01", exact: true }),
  ).toBeVisible();
  expect(queries.at(-1)).toContain("from=2026-10-22");
  expect(queries.at(-1)).toContain("to=2026-10-23");
  const promise = page.waitForEvent("download");
  await page.getByRole("button", { name: "导出本页 CSV" }).click();
  const download = await promise;
  expect(download.suggestedFilename()).toBe(
    "daily-2026-10-22-2026-10-23-page1.csv",
  );
});
