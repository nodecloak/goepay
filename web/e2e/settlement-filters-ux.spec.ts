import { test, expect } from "@playwright/test";
import { chooseKitOption } from "./kit-select-helpers";
import { settlementListFixture } from "../tests/fixtures/settlement-list";
for (const width of [1280, 375])
  for (const role of ["admin", "merchant"] as const) {
    test(`original settlement applied filters and unpaged statistics for ${role} at ${width}`, async ({
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
            user: { id: "1", role, admin_class: "super" },
            csrf_token: "local156",
          };
        else if (u.pathname.endsWith("/workflows/settlements/statistics")) {
          reads.push(u);
          data = {
            total_count: "9007199254740993",
            total_money: "90071992547409.93",
            waiting_count: "9007199254740990",
            completed_count: "1",
            processing_count: "1",
            failed_count: "1",
            cancelled_count: "0",
            returning_count: "0",
            returned_count: "0",
          };
        } else if (u.pathname.endsWith("/workflows/settlements")) {
          reads.push(u);
          data = { items: [settlementListFixture()], total: 1 };
        }
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto("/" + role + "/settle/list");
      await expect(
        page.getByRole("button", {
          name: "查看结算详情 9007199254740993",
          exact: true,
        }),
      ).toBeVisible();
      const form = page.getByRole("form", { name: "结算筛选" });
      await expect(form).toBeVisible();
      const count = reads.length;
      await form
        .getByLabel("结算账号/姓名", { exact: true })
        .fill("本地张三158");
      await chooseKitOption(
        page,
        form.getByRole("button", { name: "结算方式", exact: true }),
        "银行卡",
      );
      await form.getByRole("button", { name: "展开", exact: true }).click();
      await chooseKitOption(
        page,
        form.getByRole("button", { name: "状态", exact: true }),
        "待结算",
      );
      await form.getByLabel("开始日期").fill("2026-10-01");
      await form.getByLabel("结束日期").fill("2026-10-26");
      if (role === "admin")
        await form
          .getByLabel("商户号", { exact: true })
          .fill("9223372036854775807");
      else
        await expect(form.getByLabel("商户号", { exact: true })).toHaveCount(0);
      expect(reads.length).toBe(count);
      await form.getByRole("button", { name: "查询", exact: true }).click();
      await expect.poll(() => reads.length).toBeGreaterThan(count);
      const applied = reads.at(-1)!;
      expect(applied.searchParams.get("value")).toBe("本地张三158");
      expect(applied.searchParams.get("type")).toBe("bank");
      expect(applied.searchParams.get("dstatus")).toBe("0");
      expect(applied.searchParams.get("from")).toBe("2026-10-01");
      expect(applied.searchParams.get("to")).toBe("2026-10-26");
      if (role === "admin")
        expect(applied.searchParams.get("merchant_id")).toBe(
          "9223372036854775807",
        );
      else expect(applied.searchParams.has("merchant_id")).toBe(false);
      const opener = page.getByRole("button", {
        name: "结算统计",
        exact: true,
      });
      await opener.click();
      const dialog = page.getByRole("dialog", {
        name: "结算统计",
        exact: true,
      });
      await expect(dialog).toContainText("90071992547409.93");
      await expect(dialog).toContainText("9007199254740993");
      await expect(dialog).toContainText("正在结算");
      const stats = reads.at(-1)!;
      expect(stats.searchParams.get("value")).toBe("本地张三158");
      expect(stats.pathname).toContain("/statistics");
      expect(stats.searchParams.has("page")).toBe(false);
      expect(stats.searchParams.get("type")).toBe("bank");
      await page.screenshot({
        path: test
          .info()
          .outputPath(`settlement-statistics-${role}-${width}.png`),
        fullPage: true,
      });
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      await expect(opener).toBeFocused();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      expect(writes).toEqual([]);
    });
  }

test("invalid merchant settlement route cannot broaden until explicit reset, and failed statistics clear old totals", async ({
  page,
}) => {
  let lists = 0,
    stats = 0;
  await page.route("**/console/v1/**", (route) => {
    const u = new URL(route.request().url());
    let data: unknown = { items: [], total: 0 };
    if (u.pathname.endsWith("/session"))
      data = { user: { id: "1", role: "merchant" }, csrf_token: "local156" };
    else if (u.pathname.endsWith("/workflows/settlements/statistics")) {
      stats++;
      if (stats > 1)
        return route.fulfill({
          status: 503,
          json: { code: -1, message: "本地统计暂不可用" },
        });
      data = {
        total_count: "1",
        total_money: "99.99",
        waiting_count: "1",
        completed_count: "0",
        processing_count: "0",
        failed_count: "0",
        cancelled_count: "0",
        returning_count: "0",
        returned_count: "0",
      };
    } else if (u.pathname.endsWith("/workflows/settlements")) {
      lists++;
      expect(u.searchParams.has("merchant_id")).toBe(false);
      data = { items: [], total: 0 };
    }
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/merchant/settle/list?uid=2&type=1&dstatus=0");
  await expect(page.getByRole("alert")).toContainText("关联记录筛选参数无效");
  expect(lists).toBe(0);
  await expect(
    page.getByRole("button", { name: "结算统计", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expect(page).toHaveURL(/\/merchant\/settle\/list$/);
  await expect.poll(() => lists).toBe(1);
  const opener = page.getByRole("button", { name: "结算统计", exact: true });
  await opener.click();
  let dialog = page.getByRole("dialog", { name: "结算统计", exact: true });
  await expect(dialog).toContainText("99.99");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await opener.click();
  dialog = page.getByRole("dialog", { name: "结算统计", exact: true });
  await expect(dialog.getByRole("alert")).toContainText("本地统计暂不可用");
  await expect(dialog).not.toContainText("99.99");
  await page.keyboard.press("Escape");
  await expect(opener).toBeFocused();
});
