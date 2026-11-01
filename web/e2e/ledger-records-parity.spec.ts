import { test, expect } from "@playwright/test";

for (const area of ["admin", "merchant"] as const)
  for (const width of [375, 1280]) {
    test(`ledger ${area} at ${width} presents operational columns and safe local details`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 900 });
      const reads: string[] = [],
        writes: string[] = [];
      const entry = {
        id: "11",
        merchant_id: "1000",
        account_id: "9007199254740993123",
        ledger_type: "order_refund",
        description: "order refund",
        amount: "-1",
        balance_before: "9223372036854775807",
        balance_after: "9223372036854775806",
        kind: "available",
        created_at: "2026-11-01T01:02:03Z",
        event_key: "refund:INTERNAL-123:hold-cycle-456",
        private_key: "do-not-show-private-value",
      };
      await page.route("**/console/v1/**", async (route) => {
        const r = route.request(),
          u = new URL(r.url()),
          p = u.pathname;
        if (r.method() !== "GET") {
          writes.push(p);
          throw Error("Unexpected write");
        }
        let data: unknown = {};
        if (p.endsWith("/session"))
          data = {
            user: {
              id: area === "admin" ? "1" : "1000",
              name: "Test",
              role: area,
              admin_class: "super",
            },
            csrf_token: "synthetic-csrf",
          };
        else if (p.endsWith("/security/payment-password"))
          data = {
            configured: false,
            enforced: false,
            credential_epoch: 0,
            totp_required: false,
          };
        else if (p.endsWith("/ledger")) {
          reads.push(u.search);
          data = { items: [entry], total: 1 };
        }
        return route.fulfill({ status: 200, json: { code: 0, data } });
      });
      await page.goto(
        area === "admin" ? "/admin/merchant/record" : "/merchant/record",
      );
      const expected = [
        ...(area === "admin" ? ["商户号"] : []),
        "操作类型",
        "变更金额",
        "账户变更前",
        "账户变更后",
        "时间",
        "关联订单/业务号",
        "操作",
      ];
      await expect(page.getByRole("columnheader")).toHaveText(expected);
      await expect(
        page.getByText("不代表商户总资产", { exact: false }),
      ).toBeVisible();
      await expect(
        page.getByRole("cell", { name: entry.event_key, exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("cell", { name: entry.account_id, exact: true }),
      ).toHaveCount(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      if (area === "admin") {
        const merchant = page.getByRole("textbox", {
          name: "商户号筛选",
          exact: true,
        });
        await expect(merchant).toBeVisible();
        await merchant.fill("9007199254740993");
        await page.getByRole("button", { name: "查询", exact: true }).click();
        await expect
          .poll(() => new URLSearchParams(reads.at(-1)).get("merchant_id"))
          .toBe("9007199254740993");
        await page.getByRole("button", { name: "展开", exact: true }).click();
        await expect(
          page.getByRole("textbox", { name: "商户 ID 筛选", exact: true }),
        ).toHaveCount(0);
      } else
        await expect(
          page.getByRole("textbox", { name: "商户号筛选", exact: true }),
        ).toHaveCount(0);
      await page.screenshot({
        path: testInfo.outputPath("ledger-" + area + "-" + width + "-list.png"),
        fullPage: true,
        animations: "disabled",
      });
      const before = reads.length;
      await page
        .getByRole("button", { name: "查看资金明细 11", exact: true })
        .click();
      const dialog = page.getByRole("dialog", {
        name: "资金明细详情",
        exact: true,
      });
      await expect(dialog).toContainText(entry.account_id);
      await expect(dialog).toContainText("可用余额");
      await expect(dialog).not.toContainText(entry.private_key);
      await expect(dialog).toHaveCSS("opacity", "1");
      await expect(dialog).not.toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
      expect(reads.length).toBe(before);
      expect(writes).toEqual([]);
      await page.screenshot({
        path: testInfo.outputPath("ledger-" + area + "-" + width + ".png"),
        fullPage: true,
        animations: "disabled",
      });
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: "查看资金明细 11", exact: true }),
      ).toBeFocused();
    });
  }
