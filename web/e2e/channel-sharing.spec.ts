import { test, expect } from "@playwright/test";
import { chooseKitOption } from "./kit-select-helpers";

// GOEPAY's channel-sharing is now presented in the Pro console shape:
// /profitsharing/receiver renders ChannelSharingRuleList (one row per payment
// channel's current rule + bound receivers, add/edit in a KitModal), and
// /profitsharing/order renders ChannelSharingOrderList (server-paged records
// with a 操作台 overlay for the原支付声明 submit / 独立完结 flow). The provider
// safety model underneath is unchanged — encrypted receiver drafts, masked
// accounts, CSRF-guarded writes, versioned immutable rules, and no silent
// resend — so these specs drive the current UI and assert those guarantees.

const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const caps = {
  merchant_id: "1000",
  available: true,
  reason: "",
  channels: [
    {
      channel_id: "7",
      provider: "wxpay",
      product: "wxpay_v3_direct",
      methods: ["native"],
      receiver_types: ["MERCHANT_ID", "PERSONAL_OPENID"],
      return_supported: true,
      finish_supported: true,
    },
  ],
};
const rec = {
  id: "9",
  merchant_id: "1000",
  channel_id: "7",
  provider: "wxpay",
  product: "wxpay_v3_direct",
  type: "MERCHANT_ID",
  account_masked: "***1234",
  name_masked: "测***",
  state: "draft",
};

// A rule-list row needs the channel + (optionally) its current rule version and
// bound receivers. The list reads /channels then /receivers then /rules/<id>.
for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375])
    test(`${area} channel sharing receiver draft stays private and masks after save at ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      let receiverWrites = 0,
        ruleWrites = 0;
      const originalName = "名".repeat(100);
      let created = false,
        published = false;
      await page.route("**/console/v1/**", async (route) => {
        const request = route.request(),
          path = new URL(request.url()).pathname;
        if (path.endsWith("/session"))
          return route.fulfill(
            ok({
              user: {
                id: "1000",
                name: "Local",
                role: area,
                admin_class: "operator",
              },
              csrf_token: "local-csrf",
            }),
          );
        if (path.endsWith("/payment-password"))
          return route.fulfill(
            ok({
              configured: false,
              enforced: false,
              credential_epoch: 0,
              totp_required: false,
            }),
          );
        if (path.endsWith("/rule-overview"))
          return route.fulfill(ok({ items: [], total: "0", limit: 20 }));
        if (path.endsWith("/channels")) return route.fulfill(ok(caps));
        if (path.endsWith("/rules/7") && request.method() === "GET") {
          if (published)
            return route.fulfill(
              ok({
                id: "15",
                revision: "1",
                enabled: true,
                minimum_amount: "0",
                methods: ["native"],
                items: [
                  {
                    receiver_id: "9",
                    mode: "rate",
                    rate_bps: "100",
                    fixed_amount: "0",
                  },
                ],
              }),
            );
          return route.fulfill(ok({}, 404));
        }
        if (request.method() === "POST") {
          // Every write carries the CSRF token.
          expect(request.headers()["x-csrf-token"]).toBe("local-csrf");
          if (path.endsWith("/receivers")) {
            const body = request.postDataJSON();
            // The full account + name are sent exactly once for this create;
            // the server returns only masked values.
            expect(body).toMatchObject({
              channel_id: "7",
              account: "local_receiver79",
              name: originalName,
            });
            receiverWrites++;
            created = true;
            return route.fulfill(ok(rec));
          }
          if (path.endsWith("/receivers/9/bind"))
            return route.fulfill(ok({ ...rec, state: "bound" }));
          if (path.endsWith("/rules/7")) {
            ruleWrites++;
            published = true;
            const body = request.postDataJSON();
            expect(body).toMatchObject({
              expected: "0",
              enabled: true,
              items: [{ receiver_id: "9", mode: "rate", rate_bps: "100" }],
            });
            return route.fulfill(
              ok({
                id: "15",
                revision: "1",
                enabled: true,
                minimum_amount: "0",
                methods: ["native"],
                items: body.items,
              }),
            );
          }
          throw Error(`unexpected sharing write ${path}`);
        }
        if (path.endsWith("/receivers"))
          return route.fulfill(
            ok({
              items: created ? [{ ...rec, state: "bound" }] : [],
              next_cursor: "0",
            }),
          );
        return route.fulfill(ok({ items: [], next_cursor: "0" }));
      });
      await page.goto(`/${area}/profitsharing/receiver`);
      const panel = page.getByRole("region", { name: "分账规则", exact: true });
      if (area === "admin") {
        await page
          .getByRole("region", { name: "分账规则总览" })
          .getByRole("button", { name: "新增", exact: true })
          .click();
        const owner = page.getByRole("dialog", { name: "选择配置商户" });
        await owner.getByLabel("配置商户ID").fill("1000");
        await owner.getByRole("button", { name: "继续配置" }).click();
      } else {
        await expect(panel.getByRole("button", { name: "新增" })).toBeEnabled();
        await panel.getByRole("button", { name: "新增" }).click();
      }
      const dialog = page.getByRole("dialog", { name: "新增分账规则" });
      await expect(dialog).toBeVisible();
      await dialog.getByLabel("接收方账号").fill("local_receiver79");
      await dialog.getByLabel("接收方姓名").fill(originalName);
      await dialog.getByLabel("分账比例").fill("1");
      // Submit via the form (Enter); the 640px dialog can push the footer button
      // off a 375px viewport, so keyboard submit is the deterministic path.
      await dialog.getByLabel("分账比例").press("Enter");
      // After save the dialog closes and the list reloads with masked data.
      await expect(dialog).toBeHidden();
      await expect(panel).toContainText("***1234");
      // The private account/name never leak into the rendered list.
      const text = (await panel.textContent()) || "";
      expect(text).not.toContain("local_receiver79");
      expect(text).not.toContain(originalName);
      // Exactly one create and one rule publication — no silent duplicate write.
      expect(receiverWrites).toBe(1);
      expect(ruleWrites).toBe(1);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath("channel-sharing-receiver.png"),
        fullPage: true,
      });
    });

// When the merchant has no channel-sharing-capable channel the server returns
// available:false with a reason and an empty channel list. The 分账规则 page
// (which reads /channels) must say so honestly and never offer a create/publish
// action (fail closed, no writes). The 分账记录 page is a pure server-paged list
// and no longer carries the capability banner, so this is asserted on 规则 only.
for (const area of ["admin", "merchant"] as const)
  test(`${area} rule list reports unavailable local capability honestly`, async ({
    page,
  }) => {
    let writes = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        path = new URL(r.url()).pathname;
      if (path.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1000",
              name: "Local",
              role: area,
              admin_class: "operator",
            },
            csrf_token: "local-csrf",
          }),
        );
      if (r.method() !== "GET") writes++;
      if (path.endsWith("/rule-overview"))
        return route.fulfill(ok({ items: [], total: "0", limit: 20 }));
      if (path.endsWith("/channels"))
        return route.fulfill(
          ok({
            ...caps,
            available: false,
            reason: "模拟通道未配置渠道分账",
            channels: [],
          }),
        );
      return route.fulfill(ok({ items: [], next_cursor: "0" }));
    });
    await page.goto(`/${area}/profitsharing/receiver`);
    const panel = page.getByRole("region", { name: "分账规则", exact: true });
    if (area === "admin") {
      await page
        .getByRole("region", { name: "分账规则总览" })
        .getByRole("button", { name: "新增", exact: true })
        .click();
      const owner = page.getByRole("dialog", { name: "选择配置商户" });
      await owner.getByLabel("配置商户ID").fill("1000");
      await owner.getByRole("button", { name: "继续配置" }).click();
    }
    await expect(panel).toContainText("模拟通道未配置渠道分账");
    // No capable channel → the create action stays disabled: fail closed.
    await expect(panel.getByRole("button", { name: "新增" })).toBeDisabled();
    expect(writes).toBe(0);
  });

// The rule-list add dialog's single 保存 masks the private account on create,
// then binds, then publishes a versioned rule — in that order, each a distinct
// CSRF-guarded write. A new receiver draft is never published into a rule
// before its bind succeeds.
test("admin rule dialog masks private input and binds receiver before versioned rule publication", async ({
  page,
}) => {
  const calls: string[] = [];
  let binds = 0,
    rules = 0,
    ruleBody: any,
    created = false,
    published = false;
  await page.route("**/console/v1/**", async (route) => {
    const r = route.request(),
      p = new URL(r.url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            name: "Local Admin",
            role: "admin",
            admin_class: "operator",
          },
          csrf_token: "local-csrf",
        }),
      );
    if (p.endsWith("/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    if (p.endsWith("/rule-overview"))
      return route.fulfill(ok({ items: [], total: "0", limit: 20 }));
    if (p.endsWith("/channels")) return route.fulfill(ok(caps));
    if (p.endsWith("/rules/7") && r.method() === "GET") {
      if (published)
        return route.fulfill(
          ok({
            id: "15",
            revision: "1",
            enabled: true,
            minimum_amount: "0",
            methods: ["native"],
            items: [
              {
                receiver_id: "9",
                mode: "rate",
                rate_bps: "100",
                fixed_amount: "0",
              },
            ],
          }),
        );
      return route.fulfill(ok({}, 404));
    }
    if (r.method() === "POST") {
      expect(r.headers()["x-csrf-token"]).toBe("local-csrf");
      if (p.endsWith("/receivers")) {
        expect(r.postDataJSON().account).toBe("synthetic-private-account");
        calls.push("create");
        created = true;
        return route.fulfill(ok(rec));
      }
      if (p.endsWith("/receivers/9/bind")) {
        binds++;
        calls.push("bind");
        return route.fulfill(ok({ ...rec, state: "bound" }));
      }
      if (p.endsWith("/rules/7")) {
        rules++;
        published = true;
        calls.push("publish");
        ruleBody = r.postDataJSON();
        return route.fulfill(
          ok({ id: "15", revision: "1", enabled: true, ...ruleBody }),
        );
      }
      throw Error(`unexpected sharing write ${p}`);
    }
    if (p.endsWith("/receivers"))
      return route.fulfill(
        ok({
          items: created ? [{ ...rec, state: "bound" }] : [],
          next_cursor: "0",
        }),
      );
    return route.fulfill(ok({ items: [], next_cursor: "0" }));
  });
  await page.goto("/admin/profitsharing/receiver");
  const panel = page.getByRole("region", { name: "分账规则", exact: true });
  await page
    .getByRole("region", { name: "分账规则总览" })
    .getByRole("button", { name: "新增", exact: true })
    .click();
  const owner = page.getByRole("dialog", { name: "选择配置商户" });
  await owner.getByLabel("配置商户ID").fill("1000");
  await owner.getByRole("button", { name: "继续配置" }).click();
  const dialog = page.getByRole("dialog", { name: "新增分账规则" });
  await dialog.getByLabel("接收方账号").fill("synthetic-private-account");
  await dialog.getByLabel("接收方姓名").fill("测试姓名");
  await dialog.getByLabel("分账比例").fill("1");
  await dialog.getByRole("button", { name: "保存" }).click();
  await expect(dialog).toBeHidden();
  // Masked only: the raw account never reaches the list.
  await expect(panel.getByRole("cell", { name: "***1234" })).toBeVisible();
  expect((await panel.textContent()) || "").not.toContain(
    "synthetic-private-account",
  );
  // Ordered create → bind → publish, exactly once each.
  expect(calls).toEqual(["create", "bind", "publish"]);
  expect(binds).toBe(1);
  expect(rules).toBe(1);
  expect(ruleBody).toMatchObject({
    merchant_id: "1000",
    expected: "0",
    enabled: true,
    items: [{ receiver_id: "9", mode: "rate", rate_bps: "100" }],
  });
  await page.screenshot({
    path: "test-results/channel-sharing-rules.png",
    fullPage: true,
  });
});

// A merchant submits one channel-sharing request from the 操作台 overlay matched
// to the original payment declaration, then — when the result is unknown — may
// only QUERY the original request, never silently resend under a new number.
test("merchant 操作台 uses original declaration then queries unknown result without another submit", async ({
  page,
}) => {
  let sends = 0,
    queries = 0;
  const listRow = {
    id: "5",
    external_no: "rec-row-5",
    out_no: "",
    state: "unsent",
    share_amount: "2000",
    order_amount: "10000",
    provider: "wxpay",
    method: "native",
    rule_id: "15",
    channel_id: "7",
    created_at: "2026-10-01T00:00:00Z",
  };
  const result = {
    id: "12",
    external_no: "stable-local-ps",
    out_no: "PS" + "a".repeat(30),
    state: "unknown",
    completed: false,
    funds_released: false,
    released_amount: "0",
    items: [{ receiver_id: "9", amount: "20", state: "pending" }],
  };
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      u = new URL(r.url()),
      p = u.pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Local Merchant", role: "merchant" },
          csrf_token: "merchant-csrf",
        }),
      );
    if (p.endsWith("/channels")) {
      expect(u.searchParams.has("merchant_id")).toBe(false);
      return route.fulfill(ok(caps));
    }
    if (p.endsWith("/orders") && r.method() === "GET") {
      // The Pro list pages with offset; the 操作台 overlay reads without one.
      // Give the list its row (so 操作台 renders) and the overlay an empty set
      // (so the submitted order is the only record shown there).
      if (u.searchParams.has("offset"))
        return route.fulfill(ok({ items: [listRow], total: "1", limit: 20 }));
      return route.fulfill(ok({ items: [], next_cursor: "0" }));
    }
    if (p.includes("/payment/"))
      return route.fulfill(
        ok({
          merchant_id: "1000",
          declared: true,
          gross_amount: "100",
          merchant_net: "100",
          items: [{ receiver_id: "9", amount: "20" }],
        }),
      );
    if (r.method() === "POST") {
      expect(r.headers()["x-csrf-token"]).toBe("merchant-csrf");
      if (p.endsWith("/query")) {
        queries++;
        expect(r.postDataJSON()).toEqual({});
        return route.fulfill(
          ok({
            ...result,
            state: "partial",
            completed: true,
            funds_released: true,
            released_amount: "80",
            items: [{ receiver_id: "9", amount: "20", state: "succeeded" }],
          }),
        );
      }
      sends++;
      expect(r.postDataJSON()).toEqual({
        trade_no: "LOCAL_TRADE",
        external_no: "stable-local-ps",
      });
      return route.fulfill(ok(result));
    }
    return route.fulfill(ok({ items: [], next_cursor: "0" }));
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/merchant/profitsharing/order");
  const list = page.getByRole("region", { name: "分账记录", exact: true });
  await expect(list).toBeVisible();
  // Open the 操作台 overlay to reach the原支付声明 submit region.
  await list.getByRole("button", { name: "操作台" }).first().click();
  const panel = page.getByRole("region", { name: "渠道分账记录" });
  await expect(
    panel.getByRole("button", { name: "读取分账数据" }),
  ).toBeEnabled();
  await panel.getByLabel("平台订单号", { exact: true }).fill("LOCAL_TRADE");
  await panel.getByRole("button", { name: "核对原支付声明" }).click();
  await panel.getByLabel("业务请求号", { exact: true }).fill("stable-local-ps");
  await panel.getByRole("button", { name: "核对并提交分账" }).click();
  await panel
    .getByRole("dialog")
    .getByRole("button", { name: "确认操作" })
    .click();
  await expect(panel).toContainText("结果未知");
  await panel.getByRole("button", { name: "查询原请求" }).click();
  await expect(panel).toContainText("剩余资金已释放");
  expect(sends).toBe(1);
  expect(queries).toBe(1);
  await page.screenshot({
    path: "test-results/channel-sharing-order-mobile.png",
    fullPage: true,
  });
});
