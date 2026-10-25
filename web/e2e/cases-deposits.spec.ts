import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("local onboarding comments and submission stay distinct from upstream approval", async ({
  page,
}) => {
  const comments: Record<string, unknown>[] = [];
  const row = { id: "1", business_name: "Test business", status: "draft" };
  let submits = 0;
  await page.route("**/console/v1/**", (route) => {
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
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "token",
        }),
      );
    if (path.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "进件商户资料",
          description: "保存本地进件草稿，不代表上游商户资格",
          fields: [
            { name: "business_name", label: "经营名称", type: "string" },
            {
              name: "status",
              label: "本地状态",
              type: "string",
              read_only: true,
            },
          ],
          permissions: { create: true, update: true, delete: true },
        }),
      );
    if (path.endsWith("/history"))
      return route.fulfill(ok({ items: comments, events: [] }));
    if (path.endsWith("/comments")) {
      comments.push({
        id: "1",
        actor_name: "Merchant",
        content: req.postDataJSON().content,
      });
      return route.fulfill(ok({ recorded: true }));
    }
    if (path.endsWith("/submit")) {
      submits++;
      row.status = "under_review";
      return route.fulfill(ok({ status: row.status }));
    }
    return route.fulfill(
      ok(path.endsWith("/1") ? row : { items: [row], total: 1 }),
    );
  });
  await page.goto("/merchant/applyments/merchant");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("不代表已向上游渠道提交进件");
  await dialog.getByLabel("新增处理备注").fill("核对资料后再继续");
  await dialog.getByRole("button", { name: "保存处理备注" }).click();
  await expect(
    dialog.getByText("核对资料后再继续", { exact: true }),
  ).toBeVisible();
  expect(submits).toBe(0);
  await dialog.getByRole("button", { name: "提交平台本地审核" }).click();
  await expect(dialog.locator(".alert.success")).toContainText(
    "尚未向上游渠道提交",
  );
  expect(submits).toBe(1);
  await expect(
    dialog.getByRole("button", { name: "保存本地审核结果" }),
  ).toHaveCount(0);
});
test("supplemental admin deposit page confirms before approving the reserved release", async ({
  page,
}) => {
  let approvals = 0;
  const row = {
    id: "1",
    action: "release",
    status: "pending",
    amount: "1.00",
    merchant_id: "1000",
  };
  await page.route("**/console/v1/**", (route) => {
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
          csrf_token: "token",
        }),
      );
    if (req.method() === "POST") {
      expect(path).toBe("/console/v1/admin/workflows/deposits/1/approve");
      approvals++;
      row.status = "approved";
      return route.fulfill(ok({}));
    }
    return route.fulfill(
      ok({
        items: [row],
        total: 1,
        deposit_money: "5.00",
        pending_release_money: "1.00",
      }),
    );
  });
  await page.goto("/admin/merchant/deposit-review");
  await page.getByRole("button", { name: "核准释放", exact: true }).click();
  expect(approvals).toBe(0);
  await page.getByRole("button", { name: "确认核准", exact: true }).click();
  await expect(page.locator(".alert.success")).toContainText(
    "保证金申请已处理",
  );
  expect(approvals).toBe(1);
  await expect(
    page.getByRole("button", { name: "核准释放", exact: true }),
  ).toHaveCount(0);
});
test("internal profitsharing requests server-computed amount only after confirmation", async ({
  page,
}) => {
  let submitted: unknown;
  await page.route("**/console/v1/**", (route) => {
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
          csrf_token: "token",
        }),
      );
    if (path.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "站内分账规则",
          fields: [
            { name: "name", label: "规则名称", type: "string" },
            { name: "enabled", label: "启用", type: "boolean" },
          ],
          permissions: { create: true, update: true, delete: true },
        }),
      );
    if (path.endsWith("/execute")) {
      expect(path).toBe(
        "/console/v1/admin/workflows/profitsharing-rules/1/execute",
      );
      submitted = req.postDataJSON();
      return route.fulfill(ok({ id: "2", amount: "0.50", status: "pending" }));
    }
    const row = { id: "1", name: "Station rule", enabled: true };
    return route.fulfill(
      ok(path.endsWith("/1") ? row : { items: [row], total: 1 }),
    );
  });
  await page.goto("/admin/profitsharing/internal");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await page.getByLabel("已支付的平台订单号").fill("PAID-ORDER-1");
  await page.getByRole("button", { name: "核对并申请站内分账" }).click();
  expect(submitted).toBeUndefined();
  await page
    .getByRole("button", { name: "确认申请站内分账", exact: true })
    .click();
  await expect(
    page.getByRole("dialog").locator(".alert.success"),
  ).toContainText("金额 0.50 元");
  expect(submitted).toEqual({ trade_no: "PAID-ORDER-1" });
});
