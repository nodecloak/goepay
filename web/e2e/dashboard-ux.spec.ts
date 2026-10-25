import { test, expect } from "@playwright/test";
for (const area of ["admin", "merchant"] as const)
  for (const width of [375, 1280])
    test(`original ${area} dashboard modules consume real overview at ${width}`, async ({
      page,
    }, testInfo) => {
      const writes: string[] = [];
      await page.setViewportSize({ width, height: 1000 });
      const trend = Array.from({ length: 7 }, (_, i) => ({
        date: `2026-10-${19 + i}`,
        orders: String(i + 1),
        paid_orders: String(i),
        amount: i === 6 ? "12345.67" : "0.00",
        alipay: i === 6 ? "12345.67" : "0.00",
        wxpay: "0.00",
      }));
      await page.route("**/console/v1/**", (route) => {
        const req = route.request(),
          p = new URL(req.url()).pathname;
        let data: any = { items: [], total: 0 };
        if (req.method() !== "GET") writes.push(p);
        if (p.endsWith("/session"))
          data = {
            user: { id: "1", name: area, role: area, admin_class: "super" },
            csrf_token: "dashboard-csrf",
          };
        else if (p.endsWith("/security/payment-password"))
          data = {
            configured: false,
            enforced: false,
            credential_epoch: 0,
            totp_required: false,
          };
        else if (p.endsWith("/dashboard"))
          data = {
            orders: 100,
            paid_orders: 90,
            available_money: "12.34",
            frozen_money: "1.00",
            summary: {
              today_orders: "7",
              today_paid_orders: "6",
              today_money: "12345.67",
              yesterday_money: "2.34",
              today_settled_money: "3.45",
              yesterday_settled_money: "4.56",
              settled_money: "50.00",
              today_transfer_money: "6.78",
              yesterday_transfer_money: "7.89",
              today_register: "1",
              total_users: "10",
            },
            trend,
            payment_mix: [{ type: "alipay", orders: "6", amount: "12345.67" }],
            fee_trend:
              area === "admin"
                ? [
                    {
                      date: "2026-10-25",
                      type: "alipay",
                      fee: "1.00",
                      estimated_profit: null,
                      missing_cost_orders: "6",
                    },
                  ]
                : null,
            todos:
              area === "admin"
                ? {
                    complaints: "2",
                    merchant_audits: "3",
                    settlements: "4",
                    transfers: "5",
                  }
                : null,
            merchant:
              area === "merchant"
                ? { id: "1", name: "本地商户", pay: "1", enabled: true }
                : null,
            announcements: [
              {
                id: "2",
                title: "本地公告",
                content: "<script>本地纯文本</script>",
                created_at: "2026-10-25T00:00:00Z",
              },
            ],
            recent_settlements: [
              {
                id: "5",
                external_no: "local-settlement",
                amount: "1.23",
                status: "approved",
                payout_status: "",
                send_state: "",
                bank_state: "",
                created_at: "2026-10-25T00:00:00Z",
              },
            ],
            runtime:
              area === "admin"
                ? {
                    go: "go1.27.3",
                    postgresql: "18.0",
                    web: "Go API / nginx",
                    server_time: "2026-10-25T00:00:00Z",
                  }
                : null,
            timezone: "Asia/Shanghai",
            business_date: "2026-10-25",
            source: "local_ledger_snapshot",
          };
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      await page.goto(`/${area}/dashboard`);
      await expect(page.locator(".stat")).toHaveCount(4);
      await expect(
        page.getByRole("heading", {
          name: area === "admin" ? "支付方式收入统计" : "近7日订单数据统计",
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        page.getByRole("heading", {
          name: area === "admin" ? "今日交易统计" : "今日交易额占比",
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        page.getByRole("heading", { name: "公告通知", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("<script>本地纯文本</script>", { exact: true }),
      ).toBeVisible();
      await page
        .getByLabel("趋势查看方式", { exact: true })
        .selectOption("amount");
      await expect(
        page.getByRole("img", { name: "近7日交易额趋势图" }),
      ).toBeVisible();
      await page
        .getByRole("button", { name: "查看趋势明细", exact: true })
        .click();
      const table = page.getByRole("table", { name: "近7日趋势明细" });
      await expect(table).toContainText("12345.67");
      await expect(table.getByRole("row")).toHaveCount(8);
      if (area === "admin") {
        for (const title of ["支付方式手续费利润", "待办事项", "服务器信息"])
          await expect(
            page.getByRole("heading", { name: title, exact: true }),
          ).toBeVisible();
        await expect(
          page.getByText("成本来源不完整", { exact: false }),
        ).toBeVisible();
        await expect(
          page.getByRole("link", { name: "待审核商户 3", exact: true }),
        ).toHaveAttribute("href", "/admin/merchant/merchant-list?pay=2");
      } else {
        await expect(
          page.getByText("商户当前余额", { exact: true }),
        ).toBeVisible();
        await expect(
          page.getByRole("heading", { name: "最近结算记录", exact: true }),
        ).toBeVisible();
        await expect(
          page.getByRole("cell", { name: "待出款", exact: true }),
        ).toBeVisible();
        await expect(
          page.getByRole("heading", { name: "服务器信息", exact: true }),
        ).toHaveCount(0);
      }
      expect(writes).toHaveLength(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath("dashboard-viewport.png"),
      });
    });
