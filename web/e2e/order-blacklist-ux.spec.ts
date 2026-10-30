import { test, expect, type Page } from "@playwright/test";
async function setup(page: Page) {
  const posts: any[] = [];
  let lose = false;
  const order = {
    id: "1",
    trade_no: "BLACKLIST-ONE",
    merchant_id: "1",
    status: "paid",
    type: "alipay",
    name: "本地订单",
    amount: "100",
    client_ip: "192.0.2.55",
    buyer: "local-buyer",
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "admin", role: "admin", admin_class: "super" },
        csrf_token: "blacklist-csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/orders")) data = { items: [order], total: 1 };
    else if (p.endsWith("/orders/BLACKLIST-ONE"))
      data = {
        order,
        reporting: { client_ip: order.client_ip, buyer: order.buyer },
        admin_control: { revision: "0" },
      };
    else if (req.method() === "POST") {
      expect(req.headers()["x-csrf-token"]).toBe("blacklist-csrf");
      const body = req.postDataJSON();
      posts.push(body);
      if (lose) {
        lose = false;
        return route.abort("failed");
      }
      data = {
        id: "10",
        kind: body.kind,
        value: body.expected_value,
        enabled: true,
        expires_at: body.days ? "2026-11-25T00:00:00Z" : "",
        replayed: posts.length > 1,
      };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return {
    posts,
    loseNext: () => {
      lose = true;
    },
  };
}
test("order IP blacklist requires explicit source confirmation and preserves the original request on unknown response", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await page
    .getByRole("dialog", { name: "订单详情", exact: true })
    .getByRole("button", { name: "拉黑IP", exact: true })
    .click();
  let d = page.getByRole("dialog", { name: "添加黑名单 - IP地址" });
  await expect(d.getByLabel("IP地址", { exact: true })).toHaveValue(
    "192.0.2.55",
  );
  await expect(d.getByLabel("IP地址", { exact: true })).toHaveAttribute(
    "readonly",
    "",
  );
  expect(f.posts).toHaveLength(0);
  await d.getByLabel("有效期（天）", { exact: true }).fill("1");
  await d.getByLabel("备注", { exact: true }).fill("本地核对");
  f.loseNext();
  await d.getByRole("button", { name: "确认拉黑", exact: true }).click();
  await expect(
    d.getByRole("button", { name: "核对原请求", exact: true }),
  ).toBeVisible();
  await d
    .getByRole("button", { name: "关闭并保留原请求", exact: true })
    .click();
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await page
    .getByRole("dialog", { name: "订单详情", exact: true })
    .getByRole("button", { name: "拉黑IP", exact: true })
    .click();
  d = page.getByRole("dialog", { name: "添加黑名单 - IP地址" });
  await d.getByRole("button", { name: "核对原请求", exact: true }).click();
  await expect.poll(() => f.posts.length).toBe(2);
  expect(f.posts[1]).toEqual(f.posts[0]);
  expect(f.posts[0]).toMatchObject({
    kind: "ip",
    expected_value: "192.0.2.55",
    days: 1,
    reason: "本地核对",
  });
});
test("buyer blacklist is available from original order details with permanent expiry by default", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const detail = page.getByRole("dialog", { name: "订单详情", exact: true });
  await detail.getByRole("button", { name: "拉黑账号", exact: true }).click();
  const d = page.getByRole("dialog", { name: "添加黑名单 - 支付账号" });
  await expect(d.getByLabel("支付账号", { exact: true })).toHaveValue(
    "local-buyer",
  );
  await expect(d.getByLabel("有效期（天）", { exact: true })).toHaveValue("0");
  await d.getByRole("button", { name: "确认拉黑", exact: true }).click();
  await expect.poll(() => f.posts.length).toBe(1);
  expect(f.posts[0]).toMatchObject({
    kind: "buyer",
    expected_value: "local-buyer",
    days: 0,
  });
});
