import { test, expect } from "@playwright/test";
import {
  settlementListFixture,
  paidSettlementListFixture,
} from "../tests/fixtures/settlement-list";
for (const width of [1280, 375])
  for (const role of ["admin", "merchant"] as const) {
    test(`original ${role} settlement columns preserve frozen financial facts and readonly details at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const writes: string[] = [];
      const waiting = settlementListFixture();
      waiting.id = "1";
      waiting.status = "pending";
      waiting.external_no = "waiting-original";
      const approved = settlementListFixture();
      approved.id = "2";
      const paid = paidSettlementListFixture();
      const rows = [
        waiting,
        approved,
        { ...paid, private_extra: "NEVER_RENDER_WHOLE_RAW_RECORD" },
      ];
      await page.route("**/console/v1/**", (route) => {
        const r = route.request(),
          p = new URL(r.url()).pathname;
        if (r.method() !== "GET") writes.push(p);
        let data: unknown = { items: [], total: 0 };
        if (p.endsWith("/session"))
          data = {
            user: {
              id: role === "admin" ? "1" : paid.merchant_id,
              role,
              admin_class: "super",
            },
            csrf_token: "settlement149",
          };
        else if (p.endsWith("/workflows/settlements"))
          data = { items: rows, total: 3 };
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto("/" + role + "/settle/list");
      await expect(page.getByRole("columnheader")).toHaveText(
        role === "admin"
          ? [
              "选择",
              "ID",
              "商户号",
              "结算方式",
              "结算账号/姓名",
              "结算金额/实际到账",
              "创建时间",
              "完成时间",
              "状态",
              "操作",
            ]
          : [
              "结算方式",
              "结算账号",
              "结算金额/实际到账",
              "创建时间",
              "完成时间",
              "状态",
            ],
      );
      const first = page.locator('[data-settlement-id="1"]'),
        second = page.locator('[data-settlement-id="2"]'),
        success = page.locator('[data-settlement-id="' + paid.id + '"]');
      await expect(first).toContainText("待结算");
      await expect(second).toContainText("待结算");
      await expect(second).not.toContainText("已完成");
      await expect(second).toContainText("预计");
      await expect(success).toContainText("已完成");
      await expect(success).toContainText("90071992547409.90");
      await expect(
        success.getByRole("cell").nth(role === "admin" ? 7 : 4),
      ).toHaveText("2026-10-25 23:35:00");
      await expect(
        first.getByRole("cell").nth(role === "admin" ? 7 : 4),
      ).toHaveText("—");
      if (role === "admin") {
        await page
          .getByRole("checkbox", { name: "选择本页待审核申请", exact: true })
          .check();
        await expect(first.getByRole("checkbox")).toBeChecked();
        await expect(second.getByRole("checkbox")).toBeDisabled();
        await expect(
          page.getByRole("button", { name: /核准所选 1 条申请/ }),
        ).toBeEnabled();
        await page
          .getByRole("checkbox", { name: "选择本页待审核申请", exact: true })
          .uncheck();
      }

      const detail = success.getByRole("button", {
        name: "查看结算详情 " + paid.id,
        exact: true,
      });
      await detail.click();
      const dialog = page.getByRole("dialog", {
        name: "结算详情 " + paid.id,
        exact: true,
      });
      await expect(dialog).toContainText("original-settlement-149");
      await expect(dialog).toContainText("手续费");
      await expect(dialog).toContainText("0.03");
      await expect(dialog).toContainText("***inal");
      await expect(dialog).not.toContainText("NEVER_RENDER_WHOLE_RAW_RECORD");
      await expect(
        dialog.getByRole("button", { name: "关闭结算详情", exact: true }),
      ).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(detail).toBeFocused();
      expect(writes).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
  }

for (const width of [1280, 375])
  test(`settlement selection cannot approve an old page while a new read is pending at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let release!: () => void, arrived!: () => void;
    const gate = new Promise<void>((r) => (release = r)),
      started = new Promise<void>((r) => (arrived = r)),
      writes: string[] = [];
    const row = settlementListFixture();
    row.id = "1";
    row.status = "pending";
    await page.route("**/console/v1/**", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (request.method() !== "GET") writes.push(url.pathname);
      let data: unknown = { items: [], total: 0 };
      if (url.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "selection149",
        };
      else if (url.pathname.endsWith("/workflows/settlements")) {
        if (url.searchParams.get("page") === "2") {
          arrived();
          await gate;
        }
        data = {
          items: [
            { ...row, id: url.searchParams.get("page") === "2" ? "2" : "1" },
          ],
          total: 2,
        };
      }
      try {
        await route.fulfill({ json: { code: 0, data } });
      } catch {
        /* Request disposal. */
      }
    });
    try {
      await page.goto("/admin/settle/list?limit=1");
      await page
        .getByRole("checkbox", { name: "选择本页待审核申请", exact: true })
        .check();
      await expect(
        page.getByRole("button", { name: /核准所选 1 条申请/ }),
      ).toBeEnabled();
      await page.getByRole("button", { name: "下一页", exact: true }).click();
      await started;
      await expect(
        page.getByRole("button", { name: /核准所选 0 条申请/ }),
      ).toBeDisabled();
      release();
      await expect(page.locator('[data-settlement-id="2"]')).toBeVisible();
      expect(writes).toEqual([]);
    } finally {
      release();
    }
  });
