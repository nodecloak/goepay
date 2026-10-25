import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("platform printing saves an explicit CAS secret replacement only after confirmation and operator navigation refuses access", async ({
  page,
}) => {
  const writes: unknown[] = [];
  let revision = "3";
  await page.route("**/console/v1/**", (r) => {
    const req = r.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/printing/settings")) {
      if (req.method() === "PUT") {
        writes.push(req.postDataJSON());
        revision = "4";
      }
      return r.fulfill(
        ok({
          revision,
          enabled: false,
          provider: "local_fake",
          app_id_masked: "***1234",
          secret_present: true,
          external_execution_available: true,
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/admin/system/printing");
  await expect(
    page.getByRole("heading", { name: "平台打印配置" }),
  ).toBeVisible();
  await expect(page.getByText(/固定网关连接器可用/)).toBeVisible();
  await expect(page.getByText(/外部执行默认关闭/)).toBeVisible();
  await page.getByLabel("凭据操作").selectOption("replace");
  await page.getByLabel("应用ID").fill("synthetic-app");
  await page.getByLabel("应用密钥").fill("synthetic-printing-secret");
  await page.getByRole("button", { name: "检查并保存打印设置" }).click();
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(
    page.getByText("打印设置已保存；保存设置不会立即打印。"),
  ).toBeVisible();
  expect(writes).toEqual([
    {
      expected_revision: "3",
      enabled: false,
      provider: "local_fake",
      credential_action: "replace",
      app_id: "synthetic-app",
      secret: "synthetic-printing-secret",
    },
  ]);
  expect(
    await page.evaluate(() =>
      JSON.stringify({
        local: { ...localStorage },
        session: { ...sessionStorage },
      }),
    ),
  ).not.toContain("synthetic-printing-secret");
  await page.route("**/console/v1/admin/session", (r) =>
    r.fulfill(
      ok({
        user: { id: "2", name: "Op", role: "admin", admin_class: "operator" },
        csrf_token: "c",
      }),
    ),
  );
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "您没有访问此页面的权限" }),
  ).toBeVisible();
});

test("merchant printing is explicit owner configuration rather than a generic plaintext resource", async ({
  page,
}) => {
  const writes: unknown[] = [];
  const paths: string[] = [];
  await page.route("**/console/v1/**", (r) => {
    const req = r.request(),
      p = new URL(req.url()).pathname;
    paths.push(p);
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/account/printing")) {
      if (req.method() === "PUT") writes.push(req.postDataJSON());
      return r.fulfill(
        ok({
          revision: writes.length ? "2" : "1",
          mode: writes.length ? 2 : 0,
          copies: 1,
          voice: false,
          device_masked: "***1234",
          device_present: true,
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/merchant/account/printing");
  await page.getByLabel("自动打印范围").selectOption("2");
  await page.getByLabel("每单份数").fill("3");
  await page.getByRole("button", { name: "检查并保存打印设置" }).click();
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(
    page.getByText("打印设置已保存；保存设置不会立即打印。"),
  ).toBeVisible();
  expect(writes).toEqual([
    {
      expected_revision: "1",
      mode: 2,
      copies: 3,
      voice: false,
      device_action: "keep",
      device_id: "",
    },
  ]);
  expect(paths.some((p) => p.includes("/resources/"))).toBe(false);
});

test("paid order receipt keeps escaped text and original unknown printing blocks duplicate creation", async ({
  page,
}) => {
  let sends = 0;
  const order = {
    id: "123",
    trade_no: "123",
    name: "item",
    type: "alipay",
    amount: "100",
    status: "paid",
  };
  await page.route("**/console/v1/**", (r) => {
    const req = r.request(),
      p = new URL(req.url()).pathname;
    if (p.endsWith("/session"))
      return r.fulfill(
        ok({
          user: { id: "1", name: "Merchant", role: "merchant" },
          csrf_token: "c",
        }),
      );
    if (p.endsWith("/orders"))
      return r.fulfill(ok({ items: [order], total: 1 }));
    if (p.endsWith("/orders/123")) return r.fulfill(ok({ order }));
    if (p.endsWith("/receipt"))
      return r.fulfill(
        ok({
          trade_no: "123",
          merchant_name: "Merchant",
          name: "<cut/> item",
          amount: "100",
          type: "alipay",
          paid_at: "2026-10-23T00:00:00Z",
          remark: "local",
        }),
      );
    if (p.endsWith("/print-jobs")) {
      if (req.method() === "POST") sends++;
      return r.fulfill(
        ok({
          items: [
            {
              id: "9",
              trade_no: "123",
              request_no: "original-PJ",
              state: "unknown",
              provider: "local_fake",
              copies: 1,
              device_masked: "***1234",
            },
          ],
          next_cursor: "",
        }),
      );
    }
    return r.fulfill(ok({}));
  });
  await page.goto("/merchant/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await page.getByRole("button", { name: "查看或刷新打印收据" }).click();
  await expect(page.getByText("<cut/> item", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "创建手工打印请求" }),
  ).toBeDisabled();
  await expect(page.getByText(/打印结果未知，禁止自动重发/)).toBeVisible();
  expect(sends).toBe(0);
});
