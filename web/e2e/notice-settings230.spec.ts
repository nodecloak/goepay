import { test, expect } from "@playwright/test";
for (const width of [375, 1280])
  test(`notice cloud speaker, receipt printer and atomic templates at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const writes: { path: string; body: Record<string, unknown> }[] = [];
    const keys = [
      "wxnotice",
      "mailnotice",
      "robotnotice",
      "smsnotice",
      "voicenotice",
      "wxnotice_tpl_order",
      "wxnotice_tpl_order_name",
      "wxnotice_tpl_order_money",
    ];
    let records = [
      { id: "1", key: "wxnotice_tpl_order", value: "template230" },
      { id: "2", key: "wxnotice_tpl_order_name", value: "thing1" },
      { id: "3", key: "wxnotice_tpl_order_money", value: "amount2" },
      ...keys
        .slice(0, 5)
        .map((key, i) => ({ id: String(i + 10), key, value: "0" })),
    ];
    const hardware: Record<string, any> = {
      voice: {
        revision: "0",
        enabled: false,
        app_id_masked: "",
        secret_present: false,
      },
      printing: {
        revision: "0",
        enabled: false,
        app_id_masked: "",
        secret_present: false,
        provider: "local_fake",
        external_execution_available: true,
      },
    };
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      let data: unknown = {};
      if (p.endsWith("/session"))
        data = {
          user: {
            id: "1",
            name: "Local230",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "local230",
        };
      if (p.endsWith("/resources/settings/schema"))
        data = {
          name: "settings",
          title: "系统设置",
          fields: [
            {
              name: "key",
              label: "配置项",
              type: "select",
              required: true,
              options: keys.map((value) => ({ value, label: value })),
            },
            { name: "value", label: "配置值", type: "string", required: true },
          ],
          permissions: { create: true, update: true, delete: false },
        };
      if (p.endsWith("/resources/settings"))
        data = { items: records, total: records.length };
      for (const kind of ["voice", "printing"]) {
        if (
          p.endsWith(
            kind === "voice" ? "/settings/voice" : "/printing/settings",
          )
        ) {
          if (req.method() === "PUT") {
            const body = req.postDataJSON();
            writes.push({ path: p, body });
            hardware[kind] = {
              ...hardware[kind],
              revision: "1",
              enabled: body.enabled,
              secret_present: true,
              app_id_masked: "***230",
            };
          }
          data = hardware[kind];
        }
      }
      if (p.endsWith("/settings/notice-templates")) {
        const body = req.postDataJSON();
        writes.push({ path: p, body });
        records = records.map((r) =>
          r.key in body.values ? { ...r, value: body.values[r.key] } : r,
        );
        data = records.filter((r) => r.key in body.values);
      }
      if (p.endsWith("/business-notices"))
        data = { items: [], next_cursor: "" };
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/system/settings?tab=notice");
    await expect(
      page.getByText(
        "云喇叭和订单小票打印的开关、凭据请在各自区块保存；下方按钮与修改计数只针对本页其它普通设置。",
        { exact: true },
      ),
    ).toBeVisible();
    const voice = page.getByRole("region", {
      name: "云喇叭语音播报",
      exact: true,
    });
    await expect(
      voice.getByLabel("云喇叭agent_id", { exact: true }),
    ).toBeVisible();
    await expect(
      voice.getByLabel("云喇叭agent_secret", { exact: true }),
    ).toBeVisible();
    await voice.getByRole("switch").click();
    await voice
      .getByRole("button", { name: "保存云喇叭配置", exact: true })
      .click();
    await expect(voice.getByRole("alert")).toContainText("请先填写完整");
    expect(writes).toHaveLength(0);
    await voice
      .getByRole("button", { name: "设置云喇叭语音播报凭据", exact: true })
      .click();
    await voice.getByLabel("云喇叭agent_id", { exact: true }).fill("agent230");
    await voice
      .getByLabel("云喇叭agent_secret", { exact: true })
      .fill("secret230");
    await voice
      .getByRole("button", { name: "保存云喇叭配置", exact: true })
      .click();
    expect(writes).toHaveLength(0);
    await page
      .getByRole("dialog", { name: "确认保存云喇叭语音播报" })
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(voice.getByRole("status")).toContainText("已保存");
    expect(writes[0].body).toMatchObject({
      enabled: true,
      app_id: "agent230",
      secret: "secret230",
      expected_revision: "0",
    });
    await expect(
      voice.getByLabel("云喇叭agent_secret", { exact: true }),
    ).toHaveValue("");
    const printer = page.getByRole("region", {
      name: "订单小票打印",
      exact: true,
    });
    await expect(printer.getByLabel("APPID", { exact: true })).toBeVisible();
    await expect(
      printer.getByLabel("APPSECRET", { exact: true }),
    ).toBeVisible();
    await printer
      .getByRole("button", { name: "设置订单小票打印凭据", exact: true })
      .click();
    await printer.getByLabel("APPID", { exact: true }).fill("printer230");
    await printer
      .getByLabel("APPSECRET", { exact: true })
      .fill("printer-secret230");
    await printer.getByRole("switch").click();
    await printer
      .getByRole("button", { name: "保存小票打印配置", exact: true })
      .click();
    await page
      .getByRole("dialog", { name: "确认保存订单小票打印" })
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(printer.getByRole("status")).toContainText("已保存");
    expect(writes[1].body).toMatchObject({
      enabled: true,
      app_id: "printer230",
      secret: "printer-secret230",
      provider: "local_fake",
    });
    await page
      .getByLabel("微信订单支付成功 · 商品名称字段", { exact: true })
      .fill("amount2");
    await page
      .getByLabel("微信订单支付成功 · 金额字段", { exact: true })
      .fill("thing1");
    await page
      .getByRole("button", { name: "保存本页设置", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "已保存2项设置" }),
    ).toBeVisible();
    expect(writes).toHaveLength(3);
    expect(writes[2].body.values).toEqual({
      wxnotice_tpl_order_name: "amount2",
      wxnotice_tpl_order_money: "thing1",
    });
    expect(
      await page.evaluate(() =>
        JSON.stringify({ ...localStorage, ...sessionStorage }),
      ),
    ).not.toContain("secret230");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
