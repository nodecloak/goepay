import { test, expect } from "@playwright/test";

for (const width of [375, 1280])
  test(`administrator rule overview reads both owners without live channels at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const reads: string[] = [],
      writes: string[] = [];
    const make = (id: string, merchant: string, enabled: boolean) => ({
      rule: {
        id,
        merchant_id: merchant,
        channel_id: "7",
        revision: "1",
        provider: "wxpay",
        enabled,
        minimum_amount: "0",
        methods: ["native"],
        items: [
          {
            receiver_id: "9",
            mode: "rate",
            rate_bps: "2000",
            fixed_amount: "0",
          },
        ],
      },
      receivers: [
        {
          id: "9",
          account_masked: "***1234",
          name_masked: "测***",
          state: "bound",
        },
      ],
    });
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        url = new URL(req.url()),
        path = url.pathname;
      if (req.method() !== "GET") {
        writes.push(path);
        return route.abort();
      }
      if (path.endsWith("/session"))
        return route.fulfill({
          json: {
            code: 0,
            data: {
              user: {
                id: "1",
                role: "admin",
                admin_class: "super",
                name: "Synthetic",
              },
              csrf_token: "synthetic",
            },
          },
        });
      if (path.endsWith("/rule-overview")) {
        reads.push(url.search);
        const owner = url.searchParams.get("merchant_id");
        const items = [
          make("51", "9007199254740993", true),
          make("52", "752", false),
        ].filter((x) => !owner || x.rule.merchant_id === owner);
        return route.fulfill({
          json: { code: 0, data: { items, total: items.length, limit: 20 } },
        });
      }
      return route.fulfill({
        json: {
          code: 0,
          data: {
            items: [],
            total: 0,
            available: false,
            reason: "本地没有实时通道",
            merchant_id: "752",
            channels: [],
          },
        },
      });
    });
    await page.goto("/admin/profitsharing/receiver");
    await expect(page.locator("tbody tr")).toHaveCount(2, { timeout: 5000 });
    await expect(page.getByRole("columnheader")).toHaveText([
      "ID",
      "支付通道",
      "商户ID",
      "商户子通道",
      "分账接收方+分账比例",
      "状态",
      "操作",
    ]);
    await expect(page.locator("tbody")).toContainText("9007199254740993");
    await expect(page.locator("tbody")).toContainText("已关闭");
    await page
      .getByRole("textbox", { name: "商户ID", exact: true })
      .fill("752");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.locator("tbody tr")).toHaveCount(1);
    await page
      .getByRole("textbox", { name: "商户ID", exact: true })
      .fill("751");
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await expect.poll(() => reads.at(-1)).toContain("merchant_id=752");
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await expect(page.locator("tbody tr")).toHaveCount(2);
    await page
      .locator("tbody tr")
      .first()
      .getByRole("button", { name: "记录", exact: true })
      .click();
    await expect(page).toHaveURL(/merchant_id=9007199254740993/);
    await expect(page).toHaveURL(/rule_id=51/);
    expect(writes).toEqual([]);
  });
