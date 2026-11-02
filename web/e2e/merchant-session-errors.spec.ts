import { expect, test } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";
import { closedLoginCaptchaFixture205 } from "./fixtures/login-captcha205";

for (const width of [1280, 375]) {
  for (const path of [
    "/account/api-info",
    "/account/settings",
    "/onecode",
    "/invite",
  ]) {
    test(`merchant owner restrictions explain the next step without mutations at ${path} ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const writes: string[] = [],
        protectedReads: string[] = [];
      await page.route("**/console/v1/**", (route) => {
        const request = route.request(),
          url = new URL(request.url()),
          endpoint = url.pathname;
        if (request.method() !== "GET") {
          writes.push(endpoint);
          return route.abort();
        }
        const site = publicSiteFixture191(route);
        if (site) return site;
        const captcha = closedLoginCaptchaFixture205(route);
        if (captcha) return captcha;
        const ok = (data: unknown) =>
          route.fulfill({ json: { code: 0, data } });
        if (endpoint.endsWith("/session"))
          return ok({
            user: { id: "1000", name: "Synthetic merchant", role: "merchant" },
            csrf_token: "synthetic-224",
            delegated_by: { id: "1", name: "Synthetic administrator" },
          });
        if (endpoint.endsWith("/account/api-info"))
          return ok({
            pid: "1000",
            algorithm: "MD5",
            api_key_masked: "synthetic****mask",
            merchant_public_key: null,
            platform_public_key: "public-test-key",
          });
        if (
          /\/(account\/(profile|signature-policy)|collection-generator|referral\/(overview|token|invitees|rewards))$/.test(
            endpoint,
          )
        ) {
          protectedReads.push(endpoint);
          return route.fulfill({
            status: 403,
            json: { code: -1, message: "own merchant session required" },
          });
        }
        return ok({ items: [], total: 0 });
      });
      await page.goto("/merchant" + path);
      const main = page.locator("main");
      const explanation =
        path === "/account/api-info"
          ? main.locator(".signature-policy [role=alert]")
          : main.getByRole("alert").filter({ hasText: "商户本人登录" }).first();
      await expect(explanation).toContainText("商户本人登录");
      await expect(explanation).not.toContainText("刷新重试");
      await expect(main).not.toContainText("own merchant session required");
      await expect(page).toHaveURL(
        new RegExp("/merchant" + path.replaceAll("/", "\\/") + "$"),
      );
      expect(writes).toEqual([]);
      expect(protectedReads.length).toBeGreaterThan(0);
      expect(new Set(protectedReads).size).toBe(protectedReads.length);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    });
  }
}
