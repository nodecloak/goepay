import { test, expect, type Page } from "@playwright/test";
import actual from "./orderbatch185-http-fixture.json" with { type: "json" };
async function setup(
  page: Page,
  loss: "none" | "prepare" | "execute" = "none",
) {
  let current = structuredClone(actual.prepared),
    prepares = 0,
    executes = 0,
    reads = 0,
    external = "";
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const q = route.request(),
      url = new URL(q.url()),
      p = url.pathname;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "9007199254740993",
          role: "admin",
          name: "合成原管理员",
          admin_class: "super",
        },
        csrf_token: "batch185-csrf",
      };
    else if (p === "/console/v1/public/layout")
      data = { zhuce: "", footer: "", format: "html" };
    else if (p.endsWith("/orders") && q.method() === "GET")
      data = {
        items: [
          {
            id: actual.prepared.items[0].order_id,
            trade_no: actual.prepared.items[0].trade_no,
            status: "pending",
            amount: "100",
            refunded_amount: "0",
            type: "alipay",
            admin_revision: "0",
          },
        ],
        total: 1,
      };
    else if (p.endsWith("/orders/business-batches") && q.method() === "POST") {
      expect(q.headers()["x-csrf-token"]).toBe("batch185-csrf");
      prepares++;
      const body = q.postDataJSON();
      external = body.external_no;
      expect(body.items).toEqual([
        { trade_no: actual.prepared.items[0].trade_no },
      ]);
      current = {
        ...structuredClone(actual.prepared),
        external_no: external,
        reason: body.reason,
      };
      if (loss === "prepare" && prepares === 1) return route.abort("failed");
      data = current;
    } else if (p.endsWith("/execute")) {
      executes++;
      expect(q.postDataJSON()).toEqual({ limit: 1 });
      current = {
        ...structuredClone(actual.completed),
        external_no: external,
        reason: current.reason,
      };
      if (loss === "execute" && executes === 1) return route.abort("failed");
      data = current;
    } else if (p.includes("/orders/business-batches")) {
      reads++;
      if (url.searchParams.has("external_no"))
        expect(url.searchParams.get("external_no")).toBe(external);
      data = current;
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, message: "", data }),
    });
  });
  return { counts: () => ({ prepares, executes, reads }), errors };
}
async function open(page: Page) {
  await page.goto("/admin/trade/order");
  await page.getByLabel("全选本页订单", { exact: true }).check();
  const menu = page.getByLabel("批量操作", { exact: true });
  expect(
    (await menu.locator("option").allTextContents())
      .slice(0, 9)
      .map((x) => x.split("（")[0]?.trim()),
  ).toEqual([
    "改未完成",
    "改已完成",
    "API退款",
    "冻结订单",
    "解冻订单",
    "删除订单",
    "重新通知",
    "确认结算",
    "手动补单",
  ]);
  await menu.selectOption("query");
  await page.getByRole("button", { name: "执行批量操作", exact: true }).click();
  const d = page.getByRole("dialog", { name: "批量订单业务确认" });
  await expect(d).toBeVisible();
  await d
    .getByLabel("操作原因", { exact: true })
    .fill("本地合成原订单补单验证");
  await d.getByLabel("我已核对原订单与本批操作", { exact: true }).check();
  return d;
}
for (const width of [1440, 375])
  test(`original batch menu and actual per-item query result ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const f = await setup(page);
    const d = await open(page);
    expect(f.counts()).toEqual({ prepares: 0, executes: 0, reads: 0 });
    await d.getByRole("button", { name: "确认执行", exact: true }).click();
    await expect(d).toContainText("本批处理结束");
    await expect(d).toContainText("已处理 1 / 1");
    await expect(d.getByRole("progressbar")).toHaveAttribute("value", "1");
    expect(f.counts().prepares).toBe(1);
    expect(f.counts().executes).toBe(1);
    const b = await d.boundingBox();
    expect(b!.x).toBeGreaterThanOrEqual(0);
    expect(b!.x + b!.width).toBeLessThanOrEqual(width + 1);
    await page.screenshot({
      path: test.info().outputPath(`original-batch185-${width}.png`),
      fullPage: true,
    });
    expect(f.errors).toEqual([]);
  });
for (const loss of ["prepare", "execute"] as const)
  test(`lost ${loss} response reads original request before another submission`, async ({
    page,
  }) => {
    const f = await setup(page, loss),
      d = await open(page);
    await d.getByRole("button", { name: "确认执行", exact: true }).click();
    await expect(d.getByRole("alert")).toBeVisible();
    const before = f.counts();
    await d.getByRole("button", { name: "读取原批结果", exact: true }).click();
    await expect(d).toContainText("已读取原批次");
    expect(f.counts().prepares).toBe(before.prepares);
    expect(f.counts().executes).toBe(before.executes);
    expect(f.counts().reads).toBeGreaterThan(before.reads);
    if (loss === "prepare") {
      await d
        .getByRole("button", { name: "继续核对原请求", exact: true })
        .click();
      await expect(d).toContainText("本批处理结束");
      expect(f.counts().prepares).toBe(1);
    } else {
      await expect(d).toContainText("已处理 1 / 1");
      expect(f.counts().executes).toBe(1);
    }
    expect(f.errors).toEqual([]);
  });
