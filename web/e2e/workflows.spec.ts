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
  await page.getByRole("button", { name: "批量操作", exact: true }).click();
  await expect(
    page.getByRole("menuitem", { name: /生成结算批次（3 条）/ }),
  ).toHaveAttribute("href", "/admin/settle/batch?request_ids=1%2C2%2C3");
  await page
    .getByRole("menuitem", { name: "核准所选 3 条申请", exact: true })
    .click();
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
test("merchant original report exports the same payment type and original field names safely", async ({
  page,
}) => {
  const queries: URL[] = [];
  const row = {
    day: "2026-10-23",
    realmoney: "0.01",
    getmoney: "0.01",
    refundmoney: "0.00",
    orderCount: "1",
    successCount: "1",
    successRate: "100.00",
  };
  await page.route("**/console/v1/**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "token",
        }),
      );
    if (url.pathname.endsWith("/reports/daily")) {
      queries.push(url);
      if (url.searchParams.get("format") === "csv")
        return route.fulfill({
          contentType: "text/csv;charset=utf-8",
          headers: {
            "Content-Disposition": "attachment; filename=merchant-daily.csv",
          },
          body: "﻿日期,订单金额,分成金额,退款金额,总订单数,成功订单数,支付成功率\n合计,0.01,0.01,0.00,1,1,100.00\n",
        });
      return route.fulfill(
        ok({ items: [row], summary: { ...row, day: "合计" }, total: 1 }),
      );
    }
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/merchant/trade/daily-stat");
  await page.getByLabel("支付方式", { exact: true }).selectOption("alipay");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(
    page.locator("tbody").getByRole("cell", { name: "0.01", exact: true }),
  ).toHaveCount(2);
  expect(queries.at(-1)?.searchParams.get("type")).toBe("alipay");
  const promise = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "导出筛选结果 CSV", exact: true })
    .click();
  const download = await promise;
  expect(download.suggestedFilename()).toBe("daily-original-statistics.csv");
  await expect
    .poll(() => queries.at(-1)?.searchParams.get("format"))
    .toBe("csv");
  expect(queries.at(-1)?.searchParams.get("type")).toBe("alipay");
  expect(queries.at(-1)?.searchParams.get("view")).toBe("original");
  expect(queries.at(-1)?.searchParams.has("uid")).toBe(false);
});
