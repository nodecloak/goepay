import { test, expect, type Page } from "@playwright/test";
import { merchantPaymentStatisticsFixture as fixture } from "../tests/fixtures/merchant-payment-statistics";
const id = "9007199254740993";
async function setup(page: Page, report: (id: string) => Promise<unknown>) {
  const reads: string[] = [],
    writes: string[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname;
    if (request.method() !== "GET") writes.push(path);
    let data: unknown = { items: [], total: 0 };
    if (path.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "operator" },
        csrf_token: "local147",
      };
    else if (path.endsWith("/merchants/schema"))
      data = {
        title: "商户管理",
        fields: [{ name: "name", label: "名称", type: "string" }],
        permissions: { create: false, update: false, delete: false },
      };
    else if (path.endsWith("/resources/merchants"))
      data = {
        items: [
          { id, name: "本地商户" },
          { id: "2", name: "另一商户" },
        ],
        total: 2,
      };
    else if (path.endsWith("/reports/merchant-payments")) {
      const uid = url.searchParams.get("uid")!;
      reads.push(uid);
      data = await report(uid);
    }
    try {
      await route.fulfill({ json: { code: 0, data } });
    } catch {
      /* A closed dialog aborts its old request. */
    }
  });
  await page.goto("/admin/merchant/merchant-list");
  const open = async (uid = id) => {
    const trigger = page.getByRole("button", {
      name: "商户关联记录 " + uid,
      exact: true,
    });
    await trigger.click();
    await page.getByRole("menuitem", { name: "支付统计", exact: true }).click();
    return trigger;
  };
  return { reads, writes, open };
}
for (const width of [1280, 375]) {
  test(`original merchant statistics menu seven-day graph and exact four columns at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let invalid = false;
    const f = await setup(page, async (uid) => {
      const data = fixture(uid);
      if (invalid) data.days.today.summary.money = "0.00";
      return data;
    });
    const trigger = await f.open();
    const dialog = page.getByRole("dialog", {
      name: "商户 " + id + " 支付统计",
      exact: true,
    });
    await expect(dialog.getByRole("columnheader")).toHaveText([
      "支付方式",
      "订单金额",
      "订单数量",
      "成功率",
    ]);
    await expect(dialog.locator("tfoot")).toContainText("¥90071992547409.94");
    await expect(dialog.locator("tfoot")).toContainText("66.67%");
    await expect(dialog.getByRole("tab")).toHaveText(["今日数据", "昨日数据"]);
    const graph = dialog.getByRole("group", {
      name: "近7日支付金额趋势",
      exact: true,
    });
    await expect(graph).toBeVisible();
    await expect(graph.locator("circle")).toHaveCount(21);
    const point = graph.getByLabel("2026-10-26 支付宝 ¥90071992547409.93", {
      exact: true,
    });
    await point.focus();
    await expect(dialog.getByRole("status")).toContainText("90071992547409.93");
    await dialog
      .getByRole("button", { name: "微信支付趋势", exact: true })
      .click();
    await expect(graph.locator("circle")).toHaveCount(14);
    await dialog
      .getByRole("button", { name: "微信支付趋势", exact: true })
      .click();
    await expect(graph.locator("circle")).toHaveCount(21);
    await dialog.getByRole("tab", { name: "昨日数据", exact: true }).click();
    await expect(dialog.locator("tfoot")).toContainText("¥1.00");
    await expect(dialog.locator("tfoot")).toContainText("50.00%");
    invalid = true;
    await dialog.getByRole("button", { name: "刷新统计", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText(
      "商户统计响应格式无效",
    );
    await expect(dialog.getByRole("table")).toHaveCount(0);
    await expect(graph).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    expect(f.writes).toEqual([]);
    expect(f.reads).toEqual([id, id]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });
  test(`late merchant report cannot replace a reopened merchant scope at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let release!: () => void, arrived!: () => void;
    const gate = new Promise<void>((r) => (release = r)),
      started = new Promise<void>((r) => (arrived = r));
    const f = await setup(page, async (uid) => {
      if (uid === id) {
        arrived();
        await gate;
      }
      return fixture(uid);
    });
    await f.open();
    await started;
    await page.getByRole("button", { name: "关闭统计", exact: true }).click();
    const trigger = await f.open("2");
    const dialog = page.getByRole("dialog", {
      name: "商户 2 支付统计",
      exact: true,
    });
    await expect(dialog.getByRole("table")).toBeVisible();
    release();
    await expect(dialog.getByRole("heading")).toHaveText("商户 2 支付统计");
    await expect(dialog).not.toContainText(id);
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    expect(f.writes).toEqual([]);
  });
  test(`pending refresh retains keyboard escape and route changes clear merchant reports at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let block = false,
      release!: () => void,
      arrived!: () => void;
    const gate = new Promise<void>((r) => (release = r)),
      started = new Promise<void>((r) => (arrived = r));
    const f = await setup(page, async (uid) => {
      if (block) {
        arrived();
        await gate;
      }
      return fixture(uid);
    });
    const trigger = await f.open();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("table")).toBeVisible();
    block = true;
    await dialog.getByRole("button", { name: "刷新统计", exact: true }).click();
    await started;
    await expect(
      dialog.getByRole("button", { name: "关闭统计", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    release();
    block = false;
    await f.open();
    await expect(dialog.getByRole("table")).toBeVisible();
    await page.goto("/admin/trade/order");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(f.writes).toEqual([]);
  });
}
