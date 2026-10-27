import { test, expect } from "@playwright/test";
import fixture from "./fixtures/buyer-charge104-http.json" with { type: "json" };
import { accountSettingsReadFixture } from "./fixtures/account-settings";

// These are actual public HTTP DTOs. Mutations below are mocked; independent
// Go/PostgreSQL and signed TLS adapter tests enforce backend financial behavior.
for (const width of [1440, 375])
  for (const lost of [false, true]) {
    test(`buyer104 original fee controls, decimal text and ${lost ? "lost response recovery" : "confirmed save"} at ${width}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 1000 });
      let current = structuredClone(fixture.merchant_default),
        reads = 0;
      const writes: unknown[] = [],
        errors: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/console/v1/**", async (route) => {
        const r = route.request(),
          path = new URL(r.url()).pathname;
        let data: unknown = { items: [], total: 0 };
        if (path.endsWith("/session"))
          data = {
            user: {
              id: current.merchant_id,
              role: "merchant",
              name: "本地合成商户",
            },
            csrf_token: "synthetic-buyer104",
          };
        else if (path.endsWith("/account/buyer-fees")) {
          if (r.method() === "PATCH") {
            const body = r.postDataJSON();
            writes.push(body);
            expect(body).toEqual({
              mode: 1,
              mode_feerate: "3.125",
              expected_fingerprint: fixture.merchant_default.fingerprint,
            });
            expect(r.headers()["x-csrf-token"]).toBe("synthetic-buyer104");
            current = structuredClone(fixture.merchant_partial);
            if (lost) return route.abort("failed");
          } else reads++;
          data = current;
        } else if (path.endsWith("/account/profile"))
          data = {
            name: "本地合成商户",
            email: "buyer104@example.invalid",
            pay: "1",
          };
        else if (path.endsWith("/security/totp")) data = { enabled: false };
        else if (r.method() !== "GET")
          throw Error("unexpected mutation " + path);
        else
          data = accountSettingsReadFixture(path, current.merchant_id) ?? data;
        await route.fulfill({ json: { code: 0, data } });
      });
      await page.goto("/merchant/account/settings");
      const panel = page.getByRole("region", {
        name: "手续费承担设置",
        exact: true,
      });
      await expect(panel.getByLabel("手续费承担方")).toHaveValue("0");
      await panel.getByLabel("手续费承担方").selectOption("1");
      const rate = panel.getByLabel("买家承担费率");
      await expect(rate).toHaveAttribute("placeholder", "留空则全部由买家承担");
      await rate.fill("0.125");
      await panel
        .getByRole("button", { name: "取消修改", exact: true })
        .click();
      await expect(panel.getByLabel("手续费承担方")).toHaveValue("0");
      expect(writes).toHaveLength(0);
      await panel.getByLabel("手续费承担方").selectOption("1");
      await panel.getByLabel("买家承担费率").fill("3.125");
      await panel
        .getByRole("button", { name: "保存设置", exact: true })
        .focus();
      await page.keyboard.press("Enter");
      if (lost) {
        await expect(panel.getByRole("alert")).toContainText("结果无法确认");
        await expect(panel.locator("form")).toHaveCount(0);
        expect(writes).toHaveLength(1);
        await panel
          .getByRole("button", { name: "重新读取", exact: true })
          .click();
        await expect(panel.getByLabel("买家承担费率")).toHaveValue("3.125");
        expect(reads).toBe(2);
        expect(writes).toHaveLength(1);
      } else {
        await expect(panel.getByRole("status")).toContainText("保存成功");
        await expect(panel.getByLabel("买家承担费率")).toHaveValue("3.125");
        expect(writes).toHaveLength(1);
      }
      await panel.scrollIntoViewIfNeeded();
      const box = await panel.boundingBox();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      expect(errors).toEqual([]);
      expect(
        await page.evaluate(
          () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
        ),
      ).not.toContain("3.125");
      await panel.screenshot({
        path: info.outputPath(`buyer104-${width}-${lost}.png`),
        animations: "disabled",
      });
    });
  }
