import { test, expect } from "@playwright/test";
const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
for (const provider of ["wxpay", "alipay"] as const) {
  test(`standalone ${provider} identity requires explicit creation, status query and one reveal`, async ({
    page,
  }) => {
    const requestID = "a".repeat(64),
      token = "b".repeat(64),
      app = "synthetic-app";
    let status = "pending",
      created = 0,
      reveals = 0,
      payments = 0;
    const metadata = () => ({
      request_id: requestID,
      merchant_id: "1000",
      channel_id: "7",
      provider,
      app_id: app,
      scope: provider === "wxpay" ? "snsapi_base" : "auth_base",
      status,
      expires_at: new Date(Date.now() + 120000).toISOString(),
    });
    await page.route("**/console/v1/**", async (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: {
              id: "1",
              name: "Admin",
              role: "admin",
              admin_class: "operator",
            },
            csrf_token: "local-csrf",
          }),
        );
      if (p.endsWith("/capabilities"))
        return route.fulfill(
          ok({
            merchant_id: "1000",
            available: true,
            reason: "",
            channels: [{ channel_id: "7", provider, app_id: app }],
          }),
        );
      if (p.endsWith("/identity-lookups")) {
        expect(r.method()).toBe("POST");
        expect(r.headers()["x-csrf-token"]).toBe("local-csrf");
        expect(r.postDataJSON()).toEqual({
          merchant_id: "1000",
          channel_id: "7",
        });
        created++;
        return route.fulfill(
          ok(
            {
              ...metadata(),
              authorization_url: `https://pay.example.invalid/identity/authorize/${token}`,
            },
            201,
          ),
        );
      }
      if (p.endsWith("/reveal")) {
        expect(r.method()).toBe("POST");
        expect(r.postDataJSON()).toEqual({});
        reveals++;
        status = "revealed";
        return route.fulfill(
          ok({
            subject_id: "synthetic-private-identity",
            label: provider === "wxpay" ? "微信OpenID" : "支付宝用户标识",
            provider,
            app_id: app,
          }),
        );
      }
      if (p.endsWith(requestID)) return route.fulfill(ok(metadata()));
      if (/orders|refund|payment/.test(p)) payments++;
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    });
    await page.goto("/admin/other/get-token");
    const panel = page.getByRole("region", { name: "获取支付用户标识" });
    await panel.getByLabel("商户 ID").fill("1000");
    await panel.getByRole("button", { name: "读取授权通道" }).click();
    await panel.getByRole("combobox").selectOption("7");
    await panel.getByRole("button", { name: "创建授权请求" }).click();
    await expect(
      panel.getByRole("img", { name: "支付平台授权二维码" }),
    ).toHaveAttribute("src", /^data:image\/png;base64,/);
    await expect(
      panel.getByRole("link", { name: "打开支付平台授权" }),
    ).toHaveAttribute(
      "href",
      `https://pay.example.invalid/identity/authorize/${token}`,
    );
    expect(reveals).toBe(0);
    await expect(
      panel.getByRole("button", { name: "读取一次授权结果" }),
    ).toHaveCount(0);
    status = "verified";
    await panel.getByRole("button", { name: "查询授权状态" }).click();
    await expect(
      panel.getByRole("button", { name: "读取一次授权结果" }),
    ).toBeVisible();
    expect(reveals).toBe(0);
    await panel.getByRole("button", { name: "读取一次授权结果" }).click();
    await expect(panel.locator("output")).toHaveText(
      "synthetic-private-identity",
    );
    expect(reveals).toBe(1);
    expect(created).toBe(1);
    expect(payments).toBe(0);
    expect(
      await page.evaluate(() => localStorage.length + sessionStorage.length),
    ).toBe(0);
    await panel.getByRole("button", { name: "清除显示" }).click();
    await expect(panel).not.toContainText("synthetic-private-identity");
    await panel.getByLabel("商户 ID").fill("1001");
    await expect(
      panel.getByRole("button", { name: "查询授权状态" }),
    ).toHaveCount(0);
  });
}
