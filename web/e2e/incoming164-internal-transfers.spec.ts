import { test, expect } from "@playwright/test";
for (const width of [1440, 375])
  test(`incoming164 merchant incoming records remain read-only and fit ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const listReads: URL[] = [];
    let writes = 0;
    const rows = [
      {
        id: "9007199254741007",
        direction: "out",
        peer_merchant_id: "9007199254740993",
        amount: "100000000000.23",
        status: "pending",
        created_at: "2026-10-25T01:00:00Z",
        completed_at: null,
        remark: null,
      },
      {
        id: "9007199254741001",
        direction: "in",
        peer_merchant_id: "9007199254740993",
        amount: "1.23",
        status: "approved",
        created_at: "2026-10-25T01:00:00Z",
        completed_at: "2026-10-25T02:00:00Z",
        remark: null,
      },
    ];
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (!["127.0.0.1", "localhost"].includes(url.hostname))
        return route.abort();
      if (!url.pathname.startsWith("/console/v1/")) return route.continue();
      if (request.method() !== "GET") writes++;
      let data: unknown = { items: [], total: 0 };
      if (url.pathname.endsWith("/session"))
        data = {
          user: {
            id: "9007199254740995",
            name: "本地合成收款商户",
            role: "merchant",
          },
          csrf_token: "synthetic-incoming164",
        };
      else if (url.pathname.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (url.pathname.endsWith("/workflows/internal-transfers")) {
        listReads.push(url);
        const filtered = rows.filter(
          (row) =>
            (!url.searchParams.has("direction") ||
              row.direction === url.searchParams.get("direction")) &&
            (!url.searchParams.has("status") ||
              row.status === url.searchParams.get("status")),
        );
        data = { items: filtered, total: filtered.length };
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/merchant/account/internal-transfer");
    const table = page.getByLabel("站内转账记录", { exact: true });
    await expect(table).toBeVisible();
    await expect(table.getByRole("columnheader")).toHaveText([
      "ID",
      "方向",
      "对方商户",
      "金额",
      "状态",
      "备注",
      "提交时间",
      "完成时间",
    ]);
    const incoming = table.locator('tr[data-transfer-id="9007199254741001"]');
    await expect(incoming).toContainText("9007199254740993");
    await expect(incoming).toContainText("+1.23");
    await expect(incoming).toContainText("2026-10-25 10:00:00");
    await expect(
      table.locator('tr[data-transfer-id="9007199254741007"]'),
    ).toContainText("-100000000000.23");
    await expect(table.getByRole("button")).toHaveCount(0);
    const form = page.getByRole("form", { name: "站内转账筛选" });
    // 方向 / 状态 are KitSelect comboboxes: open the trigger, click the option.
    await form.getByLabel("方向", { exact: true }).click();
    await page.getByRole("option", { name: "转入", exact: true }).click();
    await form.getByLabel("状态", { exact: true }).click();
    await page.getByRole("option", { name: "成功", exact: true }).click();
    await form.getByRole("button", { name: "查询", exact: true }).click();
    await expect(table.locator("tbody tr[data-transfer-id]")).toHaveCount(1);
    expect(listReads.at(-1)!.searchParams.get("direction")).toBe("in");
    expect(listReads.at(-1)!.searchParams.get("status")).toBe("approved");
    await form.getByRole("button", { name: "重置", exact: true }).click();
    await expect(table.locator("tbody tr[data-transfer-id]")).toHaveCount(2);
    await expect(form).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
    expect(await table.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(
      width === 375,
    );
    expect(writes).toBe(0);
    await page.screenshot({
      path:
        (process.env.INCOMING164_ARTIFACT_DIR || "test-results/incoming164") +
        `/incoming164-${width}.png`,
      fullPage: true,
    });
  });
