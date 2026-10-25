import { test, expect } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
test("administrator receiver return requires exact funds authorization and unknown only queries the original PR", async ({
  page,
}) => {
  let sends = 0,
    queries = 0,
    originalBody: any;
  const order = {
    id: "10",
    external_no: "local-original-ps",
    out_no: "PS" + "a".repeat(30),
    state: "succeeded",
    completed: true,
    funds_released: true,
    released_amount: "80",
    items: [{ receiver_id: "9", amount: "20", state: "succeeded" }],
  };
  const returned = {
    id: "14",
    sharing_id: "10",
    receiver_id: "9",
    external_no: "local-stable-return",
    out_no: "PR" + "b".repeat(30),
    amount: "10",
    state: "unknown",
  };
  await page.route("**/console/v1/**", (route) => {
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
    if (p.endsWith("/channels"))
      return route.fulfill(
        ok({
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
        }),
      );
    if (p.endsWith("/receivers/9"))
      return route.fulfill(
        ok({
          id: "9",
          channel_id: "7",
          provider: "wxpay",
          type: "MERCHANT_ID",
          account_masked: "***1234",
          name_masked: "测***",
          state: "bound",
        }),
      );
    if (p.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: true,
          enforced: true,
          credential_epoch: 1,
          totp_required: false,
        }),
      );
    if (p.endsWith("/funds-authorizations/challenges")) {
      expect(r.postDataJSON().target).toBe(
        "/console/v1/admin/channel-sharing/returns",
      );
      originalBody = r.postDataJSON().body;
      expect(originalBody).toEqual({
        merchant_id: "1000",
        sharing_external_no: "local-original-ps",
        receiver_id: "9",
        external_no: "local-stable-return",
        amount: "10",
      });
      return route.fulfill(
        ok({
          challenge_id: "local-return-challenge",
          expires_at: new Date(Date.now() + 120000).toISOString(),
          summary: {
            recipient: "***1234",
            return_amount: "0.10",
            buyer_refund: "false",
          },
        }),
      );
    }
    if (p.endsWith("/funds-authorizations/confirm")) {
      expect(r.postDataJSON().payment_password).toBe("local-funds-password");
      return route.fulfill(
        ok({
          authorization: "one-local-return-grant",
          expires_at: new Date(Date.now() + 120000).toISOString(),
        }),
      );
    }
    if (r.method() === "POST") {
      if (p.endsWith("/query")) {
        queries++;
        expect(r.postDataJSON()).toEqual({ merchant_id: "1000" });
        expect(r.headers()["x-payment-authorization"]).toBeUndefined();
        return route.fulfill(ok({ ...returned, state: "succeeded" }));
      }
      sends++;
      expect(r.postDataJSON()).toEqual(originalBody);
      expect(r.headers()["x-payment-authorization"]).toBe(
        "one-local-return-grant",
      );
      expect(r.headers()["x-csrf-token"]).toBe("local-csrf");
      return route.fulfill(ok(returned));
    }
    return route.fulfill(
      ok({ items: p.endsWith("/orders") ? [order] : [], next_cursor: "0" }),
    );
  });
  await page.goto("/admin/profitsharing/order");
  const panel = page.getByRole("region", { name: "渠道分账记录" });
  await panel.getByLabel("商户 ID").fill("1000");
  await panel.getByRole("button", { name: "读取分账数据" }).click();
  await panel.getByRole("button", { name: "申请原接收方回退" }).click();
  await panel.getByLabel("回退金额（元）").fill("0.10");
  await panel.getByLabel("回退业务请求号").fill("local-stable-return");
  await panel.getByRole("button", { name: "核对并提交回退" }).click();
  await panel
    .getByRole("dialog")
    .getByRole("button", { name: "确认操作" })
    .click();
  const modal = page.getByRole("dialog", { name: "管理员资金二次授权" });
  await expect(modal).toContainText("***1234");
  expect(sends).toBe(0);
  await modal.getByRole("button", { name: "已核对摘要，验证资金密码" }).click();
  await modal.getByLabel("本人管理员资金密码").fill("local-funds-password");
  await modal.getByRole("button", { name: "确认资金密码" }).click();
  expect(sends).toBe(0);
  await modal.getByRole("button", { name: "执行已核对的原操作" }).click();
  await expect(
    panel.getByRole("region", { name: "分账回退记录" }),
  ).toContainText("结果未知");
  await expect(
    panel.getByRole("button", { name: "申请原接收方回退" }),
  ).toBeDisabled();
  await panel.getByRole("button", { name: "查询原回退请求" }).click();
  await expect(panel).toContainText("已回到原商户");
  expect(sends).toBe(1);
  expect(queries).toBe(1);
  expect(await page.content()).not.toContain("one-local-return-grant");
  expect(
    await page.evaluate(() =>
      Object.keys(localStorage).concat(Object.keys(sessionStorage)),
    ),
  ).toEqual([]);
});
