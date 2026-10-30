import { test, expect, type Page } from "@playwright/test";

// 支付方式 is a KitSelect custom combobox (not native <select>): open the
// trigger by its aria-label, then click the option by its label.
async function pick(page: Page, control: string, optionLabel: string) {
  await page.getByLabel(control, { exact: true }).click();
  await page.getByRole("option", { name: optionLabel, exact: true }).click();
}
const row = {
  day: "2026-10-25",
  channel: "9007199254740993",
  channelName: "通道 #9007199254740993",
  realmoney: "90071992547409.93",
  refundmoney: "2.00",
  profitmoney: null,
  orderCount: "9007199254740993",
  successCount: "3",
  successRate: "50.00",
  missing_costs: "1",
};
const columns = [
  "支付金额",
  "退款金额",
  "手续费利润",
  "总订单数",
  "成功订单数",
  "支付成功率",
];
for (const width of [1280, 375]) {
  test(`original daily and channel statistics restore seven fields and scoped filters at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const reads: URL[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url());
      if (req.method() !== "GET") writes.push(u.pathname);
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "statistics135",
        };
      else if (/\/reports\/(daily|channels)$/.test(u.pathname)) {
        reads.push(u);
        data = {
          items: [row],
          summary: { ...row, channelName: "总计" },
          from: "2026-10-20",
          to: "2026-10-26",
          missing_costs: "1",
        };
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/trade/stat/daily");
    await expect(page.getByRole("columnheader")).toHaveText([
      "日期",
      ...columns,
    ]);
    await expect.poll(() => reads.length).toBe(1);
    expect(reads[0].searchParams.get("view")).toBe("original");
    await expect(
      page.getByRole("img", { name: "近7天交易趋势" }),
    ).toBeVisible();
    const profitLegend = page.getByRole("button", {
      name: "手续费利润",
      exact: true,
    });
    await expect(profitLegend).toHaveAttribute("aria-pressed", "true");
    await profitLegend.click();
    await expect(profitLegend).toHaveAttribute("aria-pressed", "false");
    await profitLegend.click();
    await expect(profitLegend).toHaveAttribute("aria-pressed", "true");
    await expect(
      page.getByText("90071992547409.93", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("row").filter({ hasText: "2026-10-25" }),
    ).toContainText("未知");
    await pick(page, "支付方式", "支付宝");
    await page.getByLabel("通道ID", { exact: true }).fill("9007199254740993");
    await page.getByLabel("商户号", { exact: true }).fill("1");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect.poll(() => reads.length).toBe(2);
    expect(reads[1].searchParams.get("channel")).toBe("9007199254740993");
    expect(reads[1].searchParams.get("uid")).toBe("1");
    expect(reads[1].searchParams.get("type")).toBe("alipay");
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await expect.poll(() => reads.length).toBe(3);
    expect(reads[2].searchParams.get("channel")).toBeNull();
    expect(reads[2].searchParams.get("uid")).toBeNull();
    await page.goto("/admin/trade/stat/channel");
    await expect(page.getByRole("columnheader")).toHaveText([
      "支付通道",
      ...columns,
    ]);
    await expect.poll(() => reads.length).toBe(4);
    await expect(
      page.getByRole("cell", { name: "总计", exact: true }),
    ).toBeVisible();
    await page.getByLabel("查询日期（开始）").fill("2026-10-24");
    await page.getByLabel("查询日期（结束）").fill("2026-10-25");
    await page.getByLabel("商户号", { exact: true }).fill("2");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect.poll(() => reads.length).toBe(5);
    expect(reads[4].searchParams.get("from")).toBe("2026-10-24");
    expect(reads[4].searchParams.get("to")).toBe("2026-10-25");
    expect(reads[4].searchParams.get("uid")).toBe("2");
    expect(writes).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
  test(`changed statistics filters abort old results and failures clear the summary at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let calls = 0,
      release: () => void = () => {};
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/console/v1/**", async (route) => {
      const u = new URL(route.request().url());
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "statistics135",
        };
      else if (u.pathname.endsWith("/reports/channels")) {
        calls++;
        if (calls === 2) await held;
        if (calls === 3)
          return route.fulfill({
            status: 500,
            contentType: "application/json",
            body: JSON.stringify({ code: -1, message: "统计读取失败" }),
          });
        data = {
          items: [row],
          summary: { ...row, channelName: "总计" },
          missing_costs: "1",
        };
      }
      try {
        await route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      } catch {}
    });
    await page.goto("/admin/trade/stat/channel");
    await expect(
      page.getByRole("cell", { name: "总计", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect.poll(() => calls).toBe(2);
    await page.getByLabel("商户号", { exact: true }).fill("2");
    release();
    await expect(
      page.getByRole("cell", { name: "总计", exact: true }),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("统计读取失败");
    await expect(
      page.getByRole("cell", { name: "总计", exact: true }),
    ).toHaveCount(0);
  });
}
