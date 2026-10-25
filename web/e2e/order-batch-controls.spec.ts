import { test, expect, type Page } from "@playwright/test";
async function setup(page: Page) {
  const posts: any[] = [];
  let lose = false,
    gets = 0;
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "admin", role: "admin", admin_class: "super" },
        csrf_token: "batch-csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/orders") && req.method() === "GET") {
      gets++;
      data = {
        items: ["ONE", "TWO"].map((n, i) => ({
          id: String(i + 1),
          trade_no: "BATCH-" + n,
          status: "pending",
          amount: "100",
          type: "alipay",
          admin_revision: "0",
        })),
        total: 2,
      };
    } else if (req.method() === "POST") {
      expect(req.headers()["x-csrf-token"]).toBe("batch-csrf");
      const body = req.postDataJSON();
      posts.push(body);
      if (lose) {
        lose = false;
        return route.abort("failed");
      }
      data = {
        items: body.items.map((v: any) => ({
          trade_no: v.trade_no,
          control: { status: "paid", revision: "1", archived: false },
        })),
        replayed: posts.length > 1,
      };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return {
    posts,
    loseNext: () => {
      lose = true;
    },
    reads: () => gets,
  };
}
test("selected batch has explicit confirmation and preserves revisions", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/trade/order");
  await page.getByLabel("全选本页订单", { exact: true }).check();
  await page.getByLabel("批量操作", { exact: true }).selectOption("mark-paid");
  await page.getByRole("button", { name: "执行批量操作", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "批量订单操作确认" });
  await expect(dialog).toContainText("2");
  expect(f.posts).toHaveLength(0);
  await dialog.getByLabel("操作原因", { exact: true }).fill("本地核对");
  await dialog.getByLabel("我已核对选中订单", { exact: true }).check();
  await dialog.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect.poll(() => f.posts.length).toBe(1);
  expect(f.posts[0]).toMatchObject({
    action: "mark-paid",
    reason: "本地核对",
    items: [
      { trade_no: "BATCH-ONE", expected_revision: "0" },
      { trade_no: "BATCH-TWO", expected_revision: "0" },
    ],
  });
});
test("unknown batch outcome reuses all original parameters after navigation", async ({
  page,
}) => {
  const f = await setup(page);
  await page.goto("/admin/trade/order");
  await page.getByLabel("全选本页订单", { exact: true }).check();
  await page.getByLabel("批量操作", { exact: true }).selectOption("archive");
  await page.getByRole("button", { name: "执行批量操作", exact: true }).click();
  let d = page.getByRole("dialog", { name: "批量订单操作确认" });
  await d.getByLabel("操作原因", { exact: true }).fill("本地归档");
  await d.getByLabel("我已核对选中订单", { exact: true }).check();
  f.loseNext();
  await d.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(
    d.getByRole("button", { name: "核对原批结果", exact: true }),
  ).toBeVisible();
  await d
    .getByRole("button", { name: "关闭并保留原请求", exact: true })
    .click();
  await page.evaluate(() => {
    history.pushState({}, "", "/admin/dashboard");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page.getByLabel("批量操作", { exact: true })).toHaveCount(0);
  await page.evaluate(() => {
    history.pushState({}, "", "/admin/trade/order");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await page
    .getByRole("button", { name: "核对上次批量结果", exact: true })
    .click();
  d = page.getByRole("dialog", { name: "批量订单操作确认" });
  await d.getByRole("button", { name: "核对原批结果", exact: true }).click();
  await expect.poll(() => f.posts.length).toBe(2);
  expect(f.posts[1]).toEqual(f.posts[0]);
});

test("order auto refresh requires an explicit start and at least three seconds, and stops after navigation", async ({
  page,
}) => {
  await page.clock.install();
  const f = await setup(page);
  await page.goto("/admin/trade/order");
  await page.getByLabel("全选本页订单", { exact: true }).waitFor();
  const initial = f.reads();
  await page.getByRole("button", { name: "自动刷新", exact: true }).click();
  await page.getByLabel("刷新间隔（秒）", { exact: true }).fill("2");
  await page.getByRole("button", { name: "开始自动刷新", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("不能小于3秒");
  await page.clock.runFor(4000);
  expect(f.reads()).toBe(initial);
  await page.getByLabel("刷新间隔（秒）", { exact: true }).fill("3");
  await page.getByRole("button", { name: "开始自动刷新", exact: true }).click();
  await page.clock.runFor(2900);
  expect(f.reads()).toBe(initial);
  await page.clock.runFor(200);
  await expect.poll(() => f.reads()).toBe(initial + 1);
  await page.evaluate(() => {
    history.pushState({}, "", "/admin/dashboard");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(
    page.getByRole("button", { name: "自动刷新", exact: true }),
  ).toHaveCount(0);
  await page.clock.runFor(9000);
  expect(f.reads()).toBe(initial + 1);
  expect(f.posts).toHaveLength(0);
});

test("auto refresh pauses for batch confirmation and hidden tabs, then explicit stop clears the timer", async ({
  page,
}) => {
  await page.clock.install();
  const f = await setup(page);
  await page.goto("/admin/trade/order");
  await page.getByLabel("全选本页订单", { exact: true }).waitFor();
  const initial = f.reads();
  await page.getByRole("button", { name: "自动刷新", exact: true }).click();
  await page.getByLabel("刷新间隔（秒）", { exact: true }).fill("3");
  await page.getByRole("button", { name: "开始自动刷新", exact: true }).click();
  await page.getByLabel("全选本页订单", { exact: true }).check();
  await page.getByRole("button", { name: "执行批量操作", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "批量订单操作确认" });
  await dialog.waitFor();
  await page.clock.runFor(9000);
  expect(f.reads()).toBe(initial);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      configurable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.runFor(6000);
  expect(f.reads()).toBe(initial);
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", {
      value: "visible",
      configurable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.runFor(3100);
  await expect.poll(() => f.reads()).toBe(initial + 1);
  await page.getByRole("button", { name: "停止自动刷新", exact: true }).click();
  await page.clock.runFor(9000);
  expect(f.reads()).toBe(initial + 1);
  expect(f.posts).toHaveLength(0);
});
