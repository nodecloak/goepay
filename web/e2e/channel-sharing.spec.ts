import { test, expect } from "@playwright/test";
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
for (const area of ["admin", "merchant"] as const)
  for (const width of [1280, 375])
    test(`${area} V2 single sharing79 receiver fields match original product at ${width}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      let writes = 0;
      const originalName = "名".repeat(100);
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
        if (path.endsWith("/channels"))
          return route.fulfill(
            ok({
              ...caps,
              channels: [
                {
                  ...caps.channels[0],
                  product: "wxpay_v2_single_direct",
                  methods: ["native", "scan"],
                },
              ],
            }),
          );
        if (request.method() === "POST") {
          expect(path.endsWith("/receivers")).toBe(true);
          const body = request.postDataJSON();
          expect(body).toMatchObject({
            channel_id: "7",
            type: "MERCHANT_ID",
            account: "local_receiver79",
            name: originalName,
            relation_type: "SERVICE_PROVIDER",
          });
          writes++;
          return route.fulfill(
            ok({ ...rec, product: "wxpay_v2_single_direct" }),
          );
        }
        return route.fulfill(ok({ items: [], next_cursor: "0" }));
      });
      await page.goto(`/${area}/profitsharing/receiver`);
      const panel = page.getByRole("region", { name: "渠道分账规则" });
      if (area === "admin") {
        await panel.getByLabel("商户 ID").fill("1000");
        await panel
          .getByRole("button", { name: "读取分账数据", exact: true })
          .click();
      }
      await panel.getByLabel("分账通道").selectOption("7");
      await expect(panel).toContainText("单次分账");
      await expect(panel).toContainText("付款码支付");
      await expect(panel.getByLabel("接收账号")).toHaveAttribute(
        "maxlength",
        "64",
      );
      await expect(panel.getByLabel("接收方姓名")).toHaveAttribute(
        "maxlength",
        "1024",
      );
      await panel.getByLabel("接收账号").fill("local_receiver79");
      await panel.getByLabel("接收方姓名").fill(originalName);
      await panel.getByRole("button", { name: "加密保存草稿" }).click();
      await expect(panel.getByLabel("接收方姓名")).toHaveValue("");
      await expect(panel).toContainText("***1234");
      expect(await panel.textContent()).not.toContain(originalName);
      expect(writes).toBe(1);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath("wechat-v2-sharing79.png"),
        fullPage: true,
      });
    });
for (const area of ["admin", "merchant"] as const)
  for (const pageName of ["receiver", "order"] as const)
    test(`${area} original ${pageName} menu displays unavailable local capability honestly`, async ({
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
      await page.goto(`/${area}/profitsharing/${pageName}`);
      const panel = page.getByRole("region", {
        name: pageName === "receiver" ? "渠道分账规则" : "渠道分账记录",
      });
      if (area === "admin") {
        await panel.getByLabel("商户 ID").fill("1000");
        await panel
          .getByRole("button", { name: "读取分账数据", exact: true })
          .click();
      }
      await expect(panel).toContainText("模拟通道未配置渠道分账");
      if (pageName === "receiver")
        await expect(
          panel.getByRole("button", { name: "加密保存草稿" }),
        ).toBeDisabled();
      expect(writes).toBe(0);
    });
test("admin receiver creation masks private input and binds before versioned rule publication", async ({
  page,
}) => {
  let receiver: any = undefined,
    binds = 0,
    rules = 0,
    ruleBody: any;
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
    if (p.endsWith("/channels")) return route.fulfill(ok(caps));
    if (r.method() === "POST") {
      expect(r.headers()["x-csrf-token"]).toBe("local-csrf");
      if (p.endsWith("/receivers")) {
        expect(r.postDataJSON().account).toBe("synthetic-private-account");
        receiver = rec;
        return route.fulfill(ok(receiver));
      }
      if (p.endsWith("/bind")) {
        binds++;
        receiver = {
          ...rec,
          state: "bound",
          operation: {
            state: "succeeded",
            source: "remote",
            out_no: "PB" + "a".repeat(30),
          },
        };
        return route.fulfill(ok(receiver));
      }
      if (p.endsWith("/rules/7")) {
        rules++;
        ruleBody = r.postDataJSON();
        return route.fulfill(
          ok({ id: "15", revision: "1", enabled: true, ...ruleBody }),
        );
      }
      throw Error(`unexpected sharing write ${p}`);
    }
    return route.fulfill(
      ok({ items: receiver ? [receiver] : [], next_cursor: "0" }),
    );
  });
  await page.goto("/admin/profitsharing/receiver");
  const panel = page.getByRole("region", { name: "渠道分账规则" });
  await panel.getByLabel("商户 ID").fill("1000");
  await panel
    .getByRole("button", { name: "读取分账数据", exact: true })
    .click();
  await panel.getByLabel("分账通道").selectOption("7");
  await panel.getByLabel("接收账号").fill("synthetic-private-account");
  await panel.getByLabel("接收方姓名").fill("测试姓名");
  await panel.getByRole("button", { name: "加密保存草稿" }).click();
  await expect(panel.getByLabel("接收账号")).toHaveValue("");
  await expect(
    panel.getByRole("cell", { name: "***1234 测***" }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "绑定", exact: true }).click();
  await panel
    .getByRole("dialog")
    .getByRole("button", { name: "确认操作" })
    .click();
  await expect(
    panel.getByRole("cell", { name: "已绑定", exact: true }),
  ).toBeVisible();
  await panel.getByRole("button", { name: "添加已绑定接收方" }).click();
  await panel.getByRole("button", { name: "发布新版本" }).click();
  await panel
    .getByRole("dialog")
    .getByRole("button", { name: "确认操作" })
    .click();
  await expect(panel).toContainText("规则版本 1 已发布");
  expect(binds).toBe(1);
  expect(rules).toBe(1);
  expect(ruleBody).toMatchObject({
    merchant_id: "1000",
    expected: "0",
    items: [
      { receiver_id: "9", mode: "rate", rate_bps: "100", fixed_amount: "0" },
    ],
  });
  await page.screenshot({
    path: "test-results/channel-sharing-rules.png",
    fullPage: true,
  });
});
test("merchant uses original declaration then queries unknown result without another submit", async ({
  page,
}) => {
  let sends = 0,
    queries = 0;
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
