import { test, expect } from "@playwright/test";
import fs from "node:fs";
const fixture = JSON.parse(
  fs.readFileSync(
    new URL("./subchannel168b-dashboard-fixture.json", import.meta.url),
    "utf8",
  ),
);
for (const width of [1440, 375])
  test(`subchannel168b actual dashboard fee projection ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", (r) => {
      const u = new URL(r.request().url());
      if (!["127.0.0.1", "localhost"].includes(u.hostname)) return r.abort();
      if (!u.pathname.startsWith("/console/v1/")) return r.continue();
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", name: "本地合成168B费率", role: "merchant" },
          csrf_token: "synthetic168b-dashboard",
        };
      else if (u.pathname.endsWith("/dashboard")) {
        const d = {
          ...structuredClone(fixture.parent50),
          payment_channels: structuredClone(
            fixture.parent50.payment_channels,
          ) as {
            channel_id: string;
            type: string;
            fee_rate_bps: number | null;
            fee_source: string;
          }[],
        };
        d.payment_channels.push(
          {
            ...d.payment_channels[0],
            channel_id: "9007199254740993",
            fee_rate_bps: 1000,
            fee_source: "parent",
          },
          {
            ...d.payment_channels[0],
            channel_id: "9",
            fee_rate_bps: null,
            fee_source: "unknown",
          },
        );
        data = d;
      }
      return r.fulfill({ json: { code: 0, message: "", data } });
    });
    await page.goto("/merchant/dashboard");
    const table = page.getByRole("table", { name: "通道费率", exact: true });
    await expect(table).toBeVisible();
    await expect(table).toContainText("0.50%");
    await expect(table).toContainText("10.00%");
    await expect(table).toContainText("暂不可确认");
    await expect(table).toContainText("9007199254740993");
    await table.scrollIntoViewIfNeeded();
    await page.screenshot({
      path: `../.tools/subchannel168b-dashboard-final-${width}.png`,
      animations: "disabled",
    });
    expect(errors).toEqual([]);
  });
