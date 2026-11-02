import { test, expect } from "@playwright/test";
import { localCrudFixture } from "./local-crud-fixture";

test.describe("real local system settings parity228", () => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1",
    "Owned loopback simulation only",
  );
  let fixture: Awaited<ReturnType<typeof localCrudFixture>>;
  test.beforeAll(async ({ playwright }) => {
    fixture = await localCrudFixture(playwright, import.meta.url);
  });
  test.afterAll(async () => {
    await fixture?.dispose();
  });
  async function record(key: string) {
    for (let page = 1; page < 100; page++) {
      const data = await fixture.call(
        "admin",
        `/resources/settings?page=${page}&limit=100`,
      );
      const row = data.items.find((r: { key: string }) => r.key === key);
      if (row) return row;
      if (page * 100 >= data.total) return undefined;
    }
    throw Error("unexpected settings pagination");
  }
  async function seed(key: string, value: string) {
    const row = await record(key);
    await fixture.call(
      "admin",
      "/resources/settings" + (row ? "/" + row.id : ""),
      row ? "PUT" : "POST",
      { key, value },
    );
  }
  for (const width of [375, 1440]) {
    test(`17 categories and actual optional saves/reloads at ${width}px`, async ({
      page,
    }) => {
      await fixture.attach(page, "admin");
      await page.setViewportSize({ width, height: 1100 });
      await seed("blockname", "synthetic-block228");
      await seed("user_deposit_min", "12.34");
      await seed("wxnotice_tpl_order_no", "thing1");
      await page.goto("/admin/system/settings");
      for (const name of [
        "网站信息",
        "商户相关",
        "支付相关",
        "交易投诉",
        "风控检测",
        "结算规则",
        "转账付款",
        "快捷登录",
        "消息提醒",
        "实名认证",
        "OCR配置",
        "公告排版",
        "邮箱配置",
        "短信配置",
        "IP地址",
        "中转代理",
        "高级设置",
      ]) {
        await page.getByRole("tab", { name, exact: true }).click();
        await expect(page.getByRole("tabpanel")).toBeVisible();
      }
      for (const field of [
        {
          tab: "支付相关",
          label: "商品屏蔽关键词",
          key: "blockname",
          before: "synthetic-block228",
        },
        {
          tab: "商户相关",
          label: "平台最低缴纳保证金（元）",
          key: "user_deposit_min",
          before: "12.34",
        },
        {
          tab: "消息提醒",
          label: "订单支付成功 - no",
          key: "wxnotice_tpl_order_no",
          before: "thing1",
        },
      ]) {
        await page.getByRole("tab", { name: field.tab, exact: true }).click();
        const panel = page.getByRole("tabpanel");
        const control =
          field.key === "wxnotice_tpl_order_no"
            ? panel
                .locator(".setting-field")
                .filter({ has: page.locator(`#setting-label-${field.key}`) })
                .locator("input")
            : panel.getByRole("textbox", { name: field.label, exact: true });
        await expect(control).toHaveValue(field.before);
        await control.fill("");
        await panel
          .getByRole("button", { name: "保存本页设置", exact: true })
          .click();
        await expect(
          panel.getByText("本页没有未保存修改", { exact: false }),
        ).toBeVisible();
        expect((await record(field.key)).value).toBe("");
        await page.reload();
        await page.getByRole("tab", { name: field.tab, exact: true }).click();
        await expect(control).toHaveValue("");
      }
      const canonical = await record("email");
      if (canonical)
        await fixture.call(
          "admin",
          "/resources/settings/" + canonical.id,
          "DELETE",
        );
      await seed("support_email", "legacy228@example.invalid");
      await page.reload();
      await page.getByRole("tab", { name: "网站信息", exact: true }).click();
      const legacy = page.getByRole("textbox", {
        name: "客服邮箱",
        exact: true,
      });
      await expect(legacy).toHaveValue("legacy228@example.invalid");
      await legacy.fill("");
      await page
        .getByRole("tabpanel")
        .getByRole("button", { name: "保存本页设置", exact: true })
        .click();
      await expect(
        page.getByRole("textbox", { name: "联系邮箱", exact: true }),
      ).toHaveValue("");
      expect((await record("email")).value).toBe("");
      expect((await record("support_email")).value).toBe(
        "legacy228@example.invalid",
      );
      await page.getByRole("tab", { name: "支付相关", exact: true }).click();
      const block = await record("blockname");
      let damaged = 0;
      await page.route(
        `**/console/v1/admin/resources/settings/${block.id}`,
        async (route) => {
          if (route.request().method() === "PUT" && damaged === 0) {
            const actual = await route.fetch();
            expect(actual.ok()).toBeTruthy();
            damaged++;
            await route.fulfill({
              status: 200,
              contentType: "text/html",
              body: "<html>truncated response</html>",
            });
          } else await route.continue();
        },
      );
      await page
        .getByRole("textbox", { name: "商品屏蔽关键词", exact: true })
        .fill("response-proof228");
      await page
        .getByRole("tabpanel")
        .getByRole("button", { name: "保存本页设置", exact: true })
        .click();
      await expect(
        page.getByRole("alert").filter({ hasText: "刷新核对" }),
      ).toBeVisible();
      await expect(
        page
          .getByRole("tabpanel")
          .getByRole("button", { name: "保存本页设置", exact: true }),
      ).toBeDisabled();
      expect((await record("blockname")).value).toBe("response-proof228");
      expect(damaged).toBe(1);
      await page.reload();
      await page.getByRole("tab", { name: "支付相关", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "商品屏蔽关键词", exact: true }),
      ).toHaveValue("response-proof228");
      await page.getByRole("tab", { name: "IP地址", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "用户IP地址获取方式", exact: true }),
      ).toHaveAttribute("readonly", "");
      await page.getByRole("tab", { name: "高级设置", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "后台路径", exact: true }),
      ).toHaveValue("/admin");
      await page.getByRole("tab", { name: "短信配置", exact: true }).click();
      await expect(
        page.getByText("同一手机号发送间隔至少60秒", { exact: false }),
      ).toBeVisible();
    });
    test(`versioned route cancel, save, reload and stale rejection at ${width}px`, async ({
      page,
    }) => {
      await fixture.attach(page, "admin");
      await page.setViewportSize({ width, height: 1000 });
      const initial = await fixture.call("admin", "/settings/transfer-routes");
      await fixture.call("admin", "/settings/transfer-routes", "PATCH", {
        expected_fingerprint: initial.state.fingerprint,
        routes: {
          alipay: "inherit",
          wxpay: "inherit",
          qqpay: "inherit",
          bank: "inherit",
        },
      });
      const writes: string[] = [];
      page.on("request", (r) => {
        if (
          r.method() === "PATCH" &&
          new URL(r.url()).pathname.endsWith("/settings/transfer-routes")
        )
          writes.push(r.postData() || "");
      });
      await page.goto("/admin/system/settings?tab=transfer");
      const panel = page.getByRole("region", {
        name: "转账接口通道配置",
        exact: true,
      });
      await panel
        .getByRole("button", { name: "微信转账接口通道", exact: true })
        .click();
      await page.getByRole("option", { name: "手动转账", exact: true }).click();
      await panel
        .getByRole("button", { name: "保存转账通道", exact: true })
        .click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "取消", exact: true })
        .click();
      expect(writes).toHaveLength(0);
      await panel
        .getByRole("button", { name: "保存转账通道", exact: true })
        .click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "确认保存转账通道", exact: true })
        .click();
      await expect(
        panel.getByText("转账通道配置已保存", { exact: false }),
      ).toBeVisible();
      expect(writes).toHaveLength(1);
      const saved = await fixture.call("admin", "/settings/transfer-routes");
      expect(saved.state.routes.wxpay).toBe("manual");
      const stale = await fixture.request(
        "admin",
        "/settings/transfer-routes",
        "PATCH",
        JSON.parse(writes[0]!),
      );
      expect(stale.status()).toBe(409);
      await page.reload();
      await expect(
        panel.getByRole("button", { name: "微信转账接口通道", exact: true }),
      ).toHaveText("手动转账");
    });
  }
});
