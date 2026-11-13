import { test, expect } from "@playwright/test";

test.beforeEach(async ({ context, baseURL }) => {
  const localHosts = new Set(["localhost", "127.0.0.1"]);
  if (!baseURL || !localHosts.has(new URL(baseURL).hostname))
    throw Error("USDT layout checks require a loopback fixture URL");
  await context.route("**/*", async (route) => {
    if (!localHosts.has(new URL(route.request().url()).hostname))
      return route.abort();
    return route.fallback();
  });
});

for (const area of ["admin", "merchant"] as const) {
  for (const width of [375, 1440]) {
    test(`${area} USDT configuration fields fit at ${width} without writes`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.addInitScript(
        (theme) => localStorage.setItem("goepay-console-theme", theme),
        width === 375 ? "dark" : "light",
      );
      const writes: string[] = [];
      await page.route("**/console/v1/**", async (route) => {
        const request = route.request(),
          path = new URL(request.url()).pathname;
        if (request.method() !== "GET") {
          writes.push(path);
          return route.abort();
        }
        let data: unknown = { enabled: false };
        if (path.endsWith("/session"))
          data = {
            user: {
              id: "1",
              role: area,
              name: "本地布局核验",
              admin_class: "super",
              roles: ["R_SUPER"],
            },
            csrf_token: "local-crypto-layout",
          };
        else if (path.endsWith("/crypto/config"))
          data = {
            enabled: true,
            simulation: true,
            networks: [
              { network: "tron", enabled: true, fee_bps: 100 },
              { network: "ethereum", enabled: true, fee_bps: 100 },
            ],
          };
        else if (path.endsWith("/crypto/recipients"))
          data = [
            {
              id: "local-recipient",
              merchant_id: 1,
              network: "tron",
              address: "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE",
              state: "approved",
              usable_at: "2026-01-01T00:00:00Z",
            },
          ];
        else if (
          /\/crypto\/(balances|invoices|withdrawals|recipients)$/.test(path)
        )
          data = [];
        await route.fulfill({ json: { code: 0, msg: "", data } });
      });
      await page.goto(
        `/${area}/${area === "admin" ? "system" : "account"}/crypto/recipients`,
      );
      await expect(page.locator(".crypto-funds")).toHaveAttribute(
        "data-section",
        "recipients",
      );
      await expect(page.locator("form[data-form]")).toHaveCount(0);
      await page
        .getByRole("button", { name: "申请绑定地址", exact: true })
        .click();
      await expect(
        page.locator("[data-form=recipient] button[type=submit]"),
      ).toBeEnabled();
      await expect(page.locator("[data-form=recipient] select")).toBeVisible();
      await page.addStyleTag({
        content:
          "*,*::before,*::after{transition:none!important;animation:none!important}",
      });
      const geometry = await page.evaluate(() => {
        const root = document
          .querySelector(".crypto-funds")!
          .getBoundingClientRect();
        const sidebar = document
          .querySelector(".console-sidebar")!
          .getBoundingClientRect();
        const controls = [
          ...document.querySelectorAll<HTMLSelectElement>(
            '.crypto-filter select,.crypto-funds > form.card select,[data-form="recipient"] select',
          ),
        ].map((control) => control.getBoundingClientRect().width);
        return {
          left: root.left,
          sidebarRight: sidebar.right,
          controls,
          pageWidth: document.documentElement.scrollWidth,
          width: innerWidth,
        };
      });
      expect(geometry.controls.length).toBeGreaterThan(0);
      expect(geometry.pageWidth).toBeLessThanOrEqual(width);
      for (const control of geometry.controls)
        expect(control).toBeLessThanOrEqual(width === 1440 ? 280 : width - 32);
      if (width === 1440)
        expect(geometry.left).toBeGreaterThanOrEqual(geometry.sidebarRight);
      const addressField = page.locator(
        "[data-form=recipient] .crypto-address-field",
      );
      expect((await addressField.boundingBox())!.height).toBeLessThan(100);
      const recipientRow = page.locator(".crypto-table tbody tr").first();
      await expect(recipientRow).toBeVisible();
      expect((await recipientRow.boundingBox())!.height).toBeLessThan(120);
      expect(writes).toEqual([]);
    });
  }
}
