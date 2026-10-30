import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("operator and missing schema action cannot create delegation grants", async ({
  page,
}) => {
  let adminClass = "operator",
    actions = ["sso"];
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    expect(route.request().method()).toBe("GET");
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "2",
            name: "Operator",
            role: "admin",
            admin_class: adminClass,
          },
          csrf_token: "csrf",
        }),
      );
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "商户列表",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: false, update: false, delete: false },
          actions,
        }),
      );
    return route.fulfill(
      ok({ items: [{ id: "1000", name: "Target" }], total: 1 }),
    );
  });
  await page.goto("/admin/merchant/merchant-list");
  await expect(
    page.getByRole("cell", { name: "Target", exact: true }),
  ).toBeVisible();
  // 一键登录 (代登录商户) lives in the row's 更多 menu; without the right it is absent.
  const more = page.getByRole("button", {
    name: "商户关联记录 1000",
    exact: true,
  });
  await more.click();
  await expect(
    page.getByRole("menuitem", { name: "一键登录", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "代登录商户", exact: true }),
  ).toHaveCount(0);
  adminClass = "super";
  actions = [];
  await page.reload();
  await expect(
    page.getByRole("cell", { name: "Target", exact: true }),
  ).toBeVisible();
  await more.click();
  await expect(
    page.getByRole("menuitem", { name: "一键登录", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "代登录商户", exact: true }),
  ).toHaveCount(0);
});
test("unknown delegation consumption performs only merchant session verification and never repeats grant consumption", async ({
  page,
}) => {
  const calls: string[] = [];
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session")) {
      calls.push(
        p.includes("/merchant/") ? "merchant-session" : "admin-session",
      );
      return route.fulfill(
        ok(
          p.includes("/merchant/")
            ? {
                user: { id: "2000", name: "Other", role: "merchant" },
                csrf_token: "other",
              }
            : {
                user: {
                  id: "1",
                  name: "Admin",
                  role: "admin",
                  admin_class: "super",
                },
                csrf_token: "csrf",
              },
        ),
      );
    }
    if (p.endsWith("/sso/grants")) {
      calls.push("grant");
      return route.fulfill(
        ok(
          {
            grant: "one-use-token",
            expires_at: new Date(Date.now() + 10000).toISOString(),
          },
          201,
        ),
      );
    }
    if (p.endsWith("/sso/consume")) {
      calls.push("consume");
      return route.abort("failed");
    }
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "商户列表",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: false, update: false, delete: false },
          actions: ["sso"],
        }),
      );
    return route.fulfill(
      ok({ items: [{ id: "1000", name: "Target" }], total: 1 }),
    );
  });
  await page.goto("/admin/merchant/merchant-list");
  await page
    .getByRole("button", { name: "商户关联记录 1000", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "一键登录", exact: true }).click();
  await page
    .getByRole("button", { name: "确认建立代登录会话", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("不会自动重试");
  await page
    .getByRole("button", { name: "核对当前商户会话", exact: true })
    .click();
  await expect
    .poll(() => calls.filter((x) => x === "merchant-session").length)
    .toBe(2);
  expect(calls.filter((x) => x === "grant" || x === "consume")).toEqual([
    "grant",
    "consume",
  ]);
  await expect(
    page.getByRole("link", { name: "在新标签打开商户后台" }),
  ).toHaveCount(0);
});
test("income hold retry preserves freeze UUID and original unfreeze cycle while held refunds are disabled", async ({
  page,
}) => {
  const freezes: string[] = [],
    unfreezes: string[] = [];
  let state = "none",
    holdID = "H1";
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "Operator",
            role: "admin",
            admin_class: "operator",
          },
          csrf_token: "csrf",
        }),
      );
    const hold = {
      id: holdID,
      state: state === "none" ? "released" : "held",
      external_no: freezes[0],
      amount: "90",
      merchant_net: "90",
      refund_merchant_amount: "0",
      source_mode: "legacy_unprepared",
    };
    if (p.endsWith("/freeze-income")) {
      freezes.push(r.postDataJSON().external_no);
      expect(r.headers()["x-csrf-token"]).toBe("csrf");
      if (freezes.length === 1) return route.abort("failed");
      state = "held";
      return route.fulfill(ok({ ...hold, state }));
    }
    if (p.endsWith("/unfreeze-income")) {
      unfreezes.push(r.postDataJSON().hold_id);
      if (unfreezes.length === 1) {
        holdID = "H2";
        return route.abort("failed");
      }
      return route.fulfill(ok({ ...hold, id: "H1", state: "released" }));
    }
    const order = {
      trade_no: "INCOME",
      status: "paid",
      amount: "100",
      income_status: state,
    };
    return route.fulfill(
      ok(
        p.endsWith("/INCOME")
          ? {
              order,
              income_hold: state === "none" ? null : hold,
              income_freeze_amount: "90",
              can_freeze_income: state === "none",
              can_unfreeze_income: state === "held",
            }
          : { items: [order], total: 1 },
      ),
    );
  });
  await page.goto("/admin/trade/order");
  await page.getByRole("button", { name: "详情", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "订单详情" });
  await modal
    .getByRole("button", { name: "冻结订单收入", exact: true })
    .click();
  expect(freezes).toHaveLength(0);
  await expect(modal).toContainText("90");
  await expect(modal).toContainText("不是买家预授权");
  await modal
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect(modal).toContainText("保留原冻结请求号");
  await modal
    .getByRole("button", { name: "重试原冻结请求", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect(modal).toContainText("平台订单收入已冻结");
  expect(freezes[1]).toBe(freezes[0]);
  await expect(
    modal.getByRole("button", { name: "申请退款", exact: true }),
  ).toBeDisabled();
  await expect(modal).toContainText("历史冻结款尚未建立专用退款来源");
  await modal
    .getByRole("button", { name: "解冻订单收入", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认解冻收入", exact: true })
    .click();
  await expect(modal).toContainText("保留原冻结批次");
  await modal
    .getByRole("button", { name: "重试原解冻请求", exact: true })
    .click();
  await modal
    .getByRole("button", { name: "确认解冻收入", exact: true })
    .click();
  await expect.poll(() => unfreezes).toEqual(["H1", "H1"]);
  await expect(modal).toContainText("H2");
});
test("SSO is schema-gated, explains merchant replacement, consumes grant in body and renders delegated merchant banner", async ({
  page,
}) => {
  const calls: string[] = [];
  let delegated = false;
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/session"))
      return route.fulfill(
        ok(
          p.includes("/admin/")
            ? {
                user: {
                  id: "1",
                  name: "Admin",
                  role: "admin",
                  admin_class: "super",
                },
                csrf_token: "admin-csrf",
              }
            : {
                user: { id: "1000", name: "Target", role: "merchant" },
                csrf_token: "merchant-csrf",
                delegated_by: { id: "1", name: "Admin" },
              },
        ),
      );
    if (p.endsWith("/sso/grants")) {
      calls.push("grant");
      expect(r.postDataJSON()).toEqual({ merchant_id: "1000" });
      return route.fulfill(
        ok(
          {
            grant: "one-time-secret-grant",
            expires_at: "2026-10-23T03:00:00Z",
          },
          201,
        ),
      );
    }
    if (p.endsWith("/sso/consume")) {
      calls.push("consume");
      expect(r.postDataJSON()).toEqual({ grant: "one-time-secret-grant" });
      delegated = true;
      return route.fulfill(
        ok({ merchant_id: "1000", csrf_token: "merchant-csrf" }),
      );
    }
    if (p.endsWith("/schema"))
      return route.fulfill(
        ok({
          title: "商户列表",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: false, update: false, delete: false },
          actions: ["sso"],
        }),
      );
    if (p.endsWith("/dashboard"))
      return route.fulfill(
        ok({
          orders: 0,
          paid_orders: 0,
          available_money: "0.00",
          frozen_money: "0.00",
        }),
      );
    return route.fulfill(
      ok({ items: [{ id: "1000", name: "Target" }], total: 1 }),
    );
  });
  await page.goto("/admin/merchant/merchant-list");
  await page
    .getByRole("button", { name: "商户关联记录 1000", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "一键登录", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "确认代登录" })).toContainText(
    "替换当前浏览器的商户会话",
  );
  expect(calls).toEqual([]);
  await page
    .getByRole("button", { name: "确认建立代登录会话", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "在新标签打开商户后台" }),
  ).toBeVisible();
  expect(calls).toEqual(["grant", "consume"]);
  expect(delegated).toBe(true);
  expect(
    await page.evaluate(() =>
      Object.keys(localStorage).concat(Object.keys(sessionStorage)),
    ),
  ).toEqual([]);
  expect(page.url()).not.toContain("grant");
  await page.goto("/merchant/dashboard");
  await expect(page.getByText("管理员 Admin 正在代登录本商户")).toBeVisible();
});
