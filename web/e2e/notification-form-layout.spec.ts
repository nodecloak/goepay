import { test, expect } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";

for (const width of [375, 768, 1440]) {
  for (const theme of ["light", "dark"]) {
    test(`notification forms stay readable at ${width}px ${theme}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 900 });
      await page.addInitScript(
        (theme) => localStorage.setItem("goepay-console-theme", theme),
        theme,
      );
      let writes = 0;
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.route("**/console/v1/**", (route) => {
        const publicSite = publicSiteFixture191(route);
        if (publicSite) return publicSite;
        const path = new URL(route.request().url()).pathname;
        if (route.request().method() !== "GET") writes++;
        let data: unknown = {};
        if (path.endsWith("/session"))
          data = {
            user: {
              id: "1",
              name: "Local",
              role: path.includes("/merchant/") ? "merchant" : "admin",
              admin_class: "super",
            },
            csrf_token: "local-form-review",
          };
        if (path.endsWith("/notice"))
          data = {
            id: "1",
            merchant_id: "1",
            revision: "1",
            voice_order: false,
            email_masked: "",
            configured: {},
            target_masks: {},
            msgconfig: {
              order: 0,
              settle: 0,
              login: 0,
              complain: 0,
              mchrisk: 0,
              balance: 0,
              group: 0,
              domain_audit: 0,
              daily_report: 0,
              order_money: "0.00",
              balance_money: "0.00",
            },
            platform: {
              mail_robot: {},
              telegram: false,
              qqbot: false,
              voice: false,
              group_sms: false,
              wechat_templates: {},
            },
          };
        if (path.endsWith("/notice/events"))
          data = { items: [], next_cursor: "" };
        if (path.endsWith("/resources/settings/schema"))
          data = {
            name: "settings",
            title: "系统设置",
            fields: [
              {
                name: "key",
                label: "配置项",
                type: "select",
                options: [{ value: "sms_api", label: "短信网关" }],
              },
              { name: "value", label: "配置值", type: "string" },
            ],
            permissions: { create: true, update: true, delete: false },
          };
        if (path.endsWith("/resources/settings"))
          data = { items: [], total: 0 };
        if (path.endsWith("/settings/business-sms"))
          data = {
            revision: "1",
            active_provider: "aliyun",
            ready: {},
            templates: Object.fromEntries(
              ["aliyun", "qcloud", "smsbao"].flatMap((provider) =>
                ["balance", "complain", "group"].map((scene) => [
                  provider + "_" + scene,
                  "",
                ]),
              ),
            ),
          };
        if (path.endsWith("/settlement-batches"))
          data = { items: [], total: 0, next_before_id: "0" };
        return route.fulfill({ json: { code: 0, data } });
      });

      await page.goto("/merchant/account/notice");
      const matrix = page.getByRole("table", { name: "消息推送事件与方式" });
      await expect(matrix.getByRole("radio")).toHaveCount(63);
      const choices = await matrix
        .locator('input[type="radio"]')
        .evaluateAll((elements) =>
          elements.map((e) => {
            const r = e.getBoundingClientRect(),
              label = e.closest("label")!.getBoundingClientRect();
            return { width: r.width, height: r.height, target: label.height };
          }),
        );
      for (const choice of choices) {
        expect(choice.width).toBeCloseTo(16, 0);
        expect(choice.height).toBeCloseTo(16, 0);
        if (width <= 600) expect(choice.target).toBeGreaterThanOrEqual(44);
      }
      if (width <= 600) {
        expect(
          await matrix.evaluate((e) => e.scrollWidth <= e.clientWidth + 1),
        ).toBe(true);
        await expect(
          matrix.locator(".notice-channel-name").first(),
        ).toBeVisible();
        const firstRow = matrix.locator("tbody tr").first();
        const reason = firstRow.locator(".notice-matrix-reason").first();
        expect((await reason.boundingBox())!.width).toBeGreaterThan(70);
      }
      await page.screenshot({
        path: info.outputPath("merchant-notice.png"),
        fullPage: true,
      });

      await page.goto("/admin/system/settings?tab=sms");
      const sms = page.getByRole("region", {
        name: "业务短信模板",
        exact: true,
      });
      await expect(sms.getByRole("textbox")).toHaveCount(9);
      if (width <= 600) {
        for (const input of await sms.getByRole("textbox").all()) {
          const box = (await input.boundingBox())!;
          expect(box.width).toBeGreaterThan(150);
          expect(box.x + box.width).toBeLessThanOrEqual(width);
        }
        const heading = sms.locator("tbody th").first();
        const first = sms.getByRole("textbox").first();
        expect((await first.boundingBox())!.y).toBeGreaterThan(
          (await heading.boundingBox())!.y,
        );
        await expect(sms.locator("td").first()).toHaveAttribute(
          "data-label",
          "余额不足模板",
        );
      }
      await page.screenshot({
        path: info.outputPath("business-sms.png"),
        fullPage: true,
      });

      await page.goto("/admin/settle/batch");
      const status = page.locator(
        ".settlement-batches .list-filter-card select",
      );
      await expect(status).toBeVisible();
      expect((await status.boundingBox())!.width).toBeLessThanOrEqual(240);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBe(true);
      expect(writes).toBe(0);
      expect(errors).toEqual([]);
    });
  }
}
