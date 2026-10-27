import { test, expect } from "@playwright/test";
for (const width of [1280, 375])
  test(`original seven merchant columns keep exact money, both states and private detail at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: string[] = [],
      id = "9007199254740993",
      record = {
        id,
        name: "原商户145",
        effective_group_id: "9007199254740995",
        effective_group_name: "会员组",
        available_minor: "9007199254740993",
        frozen_minor: "50",
        income_held_minor: "99",
        email: "merchant145@example.invalid",
        phone: "13000000145",
        qq: "145001",
        created_at: "2026-10-25T00:00:00Z",
        url: "merchant.example.invalid",
        approved_domains: ["pay.example.invalid"],
        pay: "2",
        enabled: false,
        core_risk: {
          blocked: false,
          reason: "",
          until: null,
          action_id: "0",
          revision: "0",
        },
        login_username: "merchant-login-145",
        signature_algorithm: "MD5",
        api_key: "never-visible-api145",
        account: "never-visible-legacy-account",
        settlement_recipient: {
          state: "configured",
          config_id: "9",
          type: "alipay",
          app_id: "",
          account_masked: "***1234",
          name_masked: "***",
        },
      };
    const fields = [
      "name",
      "effective_group_id",
      "effective_group_name",
      "available_minor",
      "frozen_minor",
      "income_held_minor",
      "settlement_recipient",
      "email",
      "phone",
      "qq",
      "created_at",
      "url",
      "approved_domains",
      "pay",
      "enabled",
      "login_username",
      "signature_algorithm",
    ].map((name) => ({
      name,
      label: name,
      type:
        name === "enabled"
          ? "boolean"
          : name === "settlement_recipient" || name === "approved_domains"
            ? "json"
            : "string",
      read_only: true,
    }));
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      if (r.method() !== "GET") writes.push(p);
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "local145",
        };
      else if (p.endsWith("/merchants/schema"))
        data = {
          title: "商户列表",
          fields: [
            ...fields,
            {
              name: "api_key",
              label: "API密钥",
              type: "password",
              write_only: true,
            },
          ],
          permissions: { create: false, update: false, delete: false },
        };
      else if (p.endsWith("/merchants/" + id)) data = record;
      else if (p.endsWith("/merchants")) data = { items: [record], total: 1 };
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/merchant-list");
    await expect(page.getByRole("columnheader")).toHaveText([
      "商户号/用户组",
      "余额",
      "结算账号/姓名",
      "联系方式",
      "注册时间/域名",
      "状态",
      "操作",
    ]);
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("cell").filter({ hasText: "原商户145" }) });
    await expect(row.getByRole("cell")).toHaveCount(7);
    await expect(row).toContainText(id);
    await expect(row).toContainText("会员组（9007199254740995）");
    await expect(row).toContainText("90071992547409.93");
    await expect(row).toContainText("冻结预留：0.50");
    await expect(row).toContainText("待解冻收入：0.99");
    await expect(row).toContainText("***1234");
    await expect(row).toContainText("收款配置：待审核");
    await expect(row).toContainText("账户：已禁用");
    await expect(row).toContainText("新付款受限：配置已关闭；支付权限待审核");
    await expect(row).not.toContainText("风控有效状态待核对");
    await expect(row).toContainText("手机：13000000145");
    await expect(row).toContainText("pay.example.invalid");
    await expect(row).not.toContainText("merchant-login-145");
    await expect(row).not.toContainText("never-visible");
    await row.getByRole("button", { name: "详情", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText("merchant-login-145");
    await expect(dialog).toContainText("MD5");
    await expect(dialog).not.toContainText("never-visible");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
