import { test, expect } from "@playwright/test";
import fs from "node:fs";
const fixture = JSON.parse(
  fs.readFileSync(
    new URL("./subchannel168b-fixture.json", import.meta.url),
    "utf8",
  ),
);
for (const width of [1440, 375])
  test(`subchannel168b merchant original runtime controls ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", (route) => {
      const r = route.request(),
        u = new URL(r.url());
      if (!["127.0.0.1", "localhost"].includes(u.hostname))
        return route.abort();
      if (!u.pathname.startsWith("/console/v1/")) return route.continue();
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", name: "本地合成商户168B", role: "merchant" },
          csrf_token: "subchannel168b-synthetic-csrf",
        };
      else if (u.pathname.endsWith("/payment-subchannels/policy"))
        data = fixture.policy;
      else if (u.pathname.endsWith("/payment-subchannels"))
        data = fixture.merchant;
      else if (u.pathname.endsWith("/resources/subchannels/schema"))
        data = {
          title: "子通道资料",
          fields: [
            { name: "name", label: "名称", type: "string" },
            { name: "enabled", label: "启用", type: "boolean" },
          ],
          permissions: { create: false, update: false, delete: false },
        };
      return route.fulfill({
        status: 200,
        json: { code: 0, message: "", data },
      });
    });
    await page.goto("/merchant/subchannel");
    // 支付通道 no longer renders a page-title heading (h2 removed in 5657ce0);
    // gate on the loaded merchant subchannel row instead.
    await expect(
      page.getByText(fixture.merchant.items[0].name, { exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: `../.tools/subchannel168b-merchant-${process.env.SUBCHANNEL168B_SCREEN_STAGE || "after"}-${width}.png`,
      animations: "disabled",
    });
    await info.attach("subchannel168b-page-errors", {
      body: JSON.stringify(errors),
      contentType: "application/json",
    });
    await expect(
      page.getByRole("button", { name: "轮询设置", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: "成功率", exact: true }),
    ).toBeVisible();
    expect(errors).toEqual([]);
  });
