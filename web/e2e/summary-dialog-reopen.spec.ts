import { expect, test } from "@playwright/test";

const summaries = [
  {
    path: "/admin/merchant/record",
    endpoint: "/ledger/summary",
    opener: "统计",
    dialog: "资金明细统计",
    data: {
      totalCount: "1",
      increaseMoney: "1.23",
      decreaseMoney: "0.00",
      netMoney: "1.23",
    },
  },
  {
    path: "/admin/trade/order",
    endpoint: "/orders/summary",
    opener: "统计",
    dialog: "订单统计概况",
    data: {
      totalCount: "1",
      paidCount: "1",
      unpaidCount: "0",
      refundedCount: "0",
      successRate: "100.00%",
      orderMoney: "1.23",
      paidMoney: "1.23",
      merchantMoney: "1.23",
      refundedMoney: "0.00",
    },
  },
  {
    path: "/admin/trade/refund",
    endpoint: "/refunds/summary",
    opener: "退款统计",
    dialog: "退款统计",
    data: {
      totalCount: "1",
      totalMoney: "1.23",
      successCount: "1",
      successMoney: "1.23",
      pendingCount: "0",
      pendingMoney: "0.00",
    },
  },
];

for (const summary of summaries) {
  test(`${summary.dialog} recovers on reopening after a failed read`, async ({
    page,
  }) => {
    const reads: string[] = [];
    const writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (request.method() !== "GET") {
        writes.push(url.pathname);
        return route.abort();
      }
      let data: unknown = { items: [], total: 0 };
      if (url.pathname.endsWith("/session")) {
        data = {
          user: {
            id: "1",
            name: "Local admin",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "local-summary",
        };
      } else if (url.pathname.endsWith(summary.endpoint)) {
        reads.push(url.href);
        if (reads.length === 1)
          return route.fulfill({
            status: 503,
            json: { code: -1, message: "统计暂不可用" },
          });
        data = summary.data;
      }
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto(summary.path);
    const opener = page.getByRole("button", {
      name: summary.opener,
      exact: true,
    });
    const dialog = page.getByRole("dialog", {
      name: summary.dialog,
      exact: true,
    });
    await opener.click();
    await expect(dialog.getByRole("alert")).toContainText("关闭后重新打开");
    await expect(
      dialog.getByRole("button", { name: /重试|刷新|读取/ }),
    ).toHaveCount(0);
    expect(reads).toHaveLength(1);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    await opener.click();
    await expect(dialog.getByRole("alert")).toHaveCount(0);
    await expect(dialog).toContainText("1.23");
    expect(reads).toHaveLength(2);
    expect(reads[1]).toBe(reads[0]);
    expect(writes).toEqual([]);
  });
}
