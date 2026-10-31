import { test, expect, type Page } from "@playwright/test";
const ok = (data: unknown) => ({
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data, message: "" }),
});
async function fixture(page: Page, status = "paid") {
  let marker = "",
    archived = false,
    revision = "0";
  const posts: { path: string; body: Record<string, unknown> }[] = [];
  const order = {
    id: "1",
    trade_no: "TUX-001",
    merchant_id: "1000",
    out_trade_no: "business-original",
    name: "测试订单",
    type: "alipay",
    amount: "1000",
    status,
  };
  await page.route("**/console/v1/**", async (route) => {
    const u = new URL(route.request().url()),
      p = u.pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            username: "admin",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "csrf-ux",
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
    if (p.endsWith("/orders/TUX-001") && route.request().method() === "GET")
      return route.fulfill(
        ok({
          order,
          reporting: { refunded_amount: "0" },
          admin_control: { status: marker, archived, revision },
          can_freeze_income: status === "paid",
          income_freeze_amount: "1000",
          can_resend_notification: status === "paid",
          can_manual_refund: status === "paid",
        }),
      );
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON();
      posts.push({ path: p, body });
      expect(route.request().headers()["x-csrf-token"]).toBe("csrf-ux");
      if (p.endsWith("/admin-status")) {
        marker = body.status;
        revision = String(Number(revision) + 1);
      }
      if (p.endsWith("/archive")) {
        archived = true;
        revision = String(Number(revision) + 1);
      }
      if (p.endsWith("/restore")) {
        archived = false;
        revision = String(Number(revision) + 1);
      }
      return route.fulfill(ok({ status: marker, archived, revision }));
    }
    if (p.endsWith("/orders"))
      return route.fulfill(
        ok({
          items:
            archived && u.searchParams.get("include_archived") !== "true"
              ? []
              : [
                  {
                    ...order,
                    admin_status: marker,
                    admin_archived: archived,
                    admin_revision: revision,
                  },
                ],
          total:
            archived && u.searchParams.get("include_archived") !== "true"
              ? 0
              : 1,
        }),
      );
    return route.fulfill(ok({}));
  });
  return posts;
}
for (const width of [375, 1280]) {
  test(`original paid and unpaid menus, viewport ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const posts = await fixture(page);
    await page.goto("/admin/trade/order");
    await page
      .getByRole("button", { name: "订单操作 TUX-001", exact: true })
      .click();
    const menu = page.getByRole("menu", { name: "订单操作 TUX-001" });
    await expect(menu.getByRole("menuitem")).toHaveText([
      "改未完成",
      "API退款",
      "手动退款",
      "冻结订单",
      "重新通知",
      "删除订单",
    ]);
    const box = await menu.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    expect(box!.y + box!.height).toBeLessThanOrEqual(900);
    expect(posts).toHaveLength(0);
    await menu.getByRole("menuitem", { name: "改未完成", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "改未完成确认",
      exact: true,
    });
    await expect(dialog).toContainText("不增减商户余额");
    expect(posts).toHaveLength(0);
    await dialog.getByLabel("操作原因").fill("人工标记待核实");
    await dialog.getByRole("button", { name: "确认修改状态" }).click();
    await expect(dialog).toContainText("后台状态标记已更新");
    await dialog
      .getByRole("button", { name: "关闭", exact: true })
      .first()
      .click();
    await page
      .getByRole("button", { name: "订单操作 TUX-001", exact: true })
      .click();
    await expect(menu.getByRole("menuitem")).toHaveText([
      "改已完成",
      "手动补单",
      "删除订单",
    ]);
    expect(posts[0]).toMatchObject({
      path: "/console/v1/admin/orders/TUX-001/admin-status",
      body: { status: "pending", expected_revision: "0" },
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}
test("logical delete can be reviewed and restored without a financial call", async ({
  page,
}) => {
  const posts = await fixture(page);
  await page.goto("/admin/trade/order");
  await page.getByRole("button", { name: "订单操作 TUX-001" }).click();
  await page.getByRole("menuitem", { name: "删除订单", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "删除订单确认" });
  await expect(dialog).toContainText("账务、退款和支付查询依据保留");
  await dialog.getByLabel("操作原因").fill("隐藏测试订单");
  expect(posts).toHaveLength(0);
  await dialog.getByRole("button", { name: "确认删除订单" }).click();
  await dialog
    .getByRole("button", { name: "关闭", exact: true })
    .first()
    .click();
  await expect(
    page.getByRole("button", { name: "订单操作 TUX-001" }),
  ).toHaveCount(0);
  await page.getByLabel("显示已删除订单").check();
  await page.getByRole("button", { name: "订单操作 TUX-001" }).click();
  await page.getByRole("menuitem", { name: "恢复订单" }).click();
  dialog = page.getByRole("dialog", { name: "恢复订单确认" });
  await dialog.getByLabel("操作原因").fill("恢复测试订单");
  await dialog.getByRole("button", { name: "确认恢复订单" }).click();
  await expect(dialog).toContainText("订单已恢复显示");
  expect(posts.map((x) => x.path.split("/").pop())).toEqual([
    "archive",
    "restore",
  ]);
});
test("offline refund prompts for mandatory funds password even when global enforcement is off", async ({
  page,
}) => {
  const posts = await fixture(page);
  await page.goto("/admin/trade/order");
  await page.getByRole("button", { name: "订单操作 TUX-001" }).click();
  await page.getByRole("menuitem", { name: "手动退款", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "手动退款确认" });
  await dialog.getByLabel("操作原因").fill("已在线下退还");
  await dialog.getByLabel("我确认已完成线下退款", { exact: false }).check();
  await dialog.getByRole("button", { name: "确认手动退款" }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "尚未设置本人管理员资金密码",
  );
  expect(posts).toHaveLength(0);
});
