import {
  test,
  expect,
  type BrowserContext,
  type Route,
} from "@playwright/test";

function records() {
  return Array.from({ length: 60 }, (_, index) => ({
    id: String(101 + index),
    merchant_id: index % 2 ? "752" : "751",
    subchannel_id: index === 1 ? "9007199254740993" : "0",
    external_no: index < 2 ? "same-number" : `sharing_${101 + index}`,
    out_no: `PS${101 + index}`,
    state: "succeeded",
    share_amount: "20",
    order_amount: "100",
    provider: "wxpay",
    method: "native",
    rule_id: index % 2 ? "6" : "5",
    channel_id: "7",
    created_at: "2026-11-01T01:02:03Z",
  }));
}
async function fixture(
  context: BrowserContext,
  returnWrite?: (route: Route, body: any) => Promise<void>,
) {
  const rows = records(),
    queries: URL[] = [],
    writes: { path: string; body: any }[] = [];
  const ok = (route: Route, data: unknown) =>
    route.fulfill({ json: { code: 0, data } });
  const detail = (row: ReturnType<typeof records>[number]) => ({
    ...row,
    completed: true,
    funds_released: true,
    released_amount: "80",
    items: [{ receiver_id: "9", amount: "20", state: "succeeded" }],
  });
  const filtered = (q: URLSearchParams) =>
    rows.filter(
      (r) =>
        (!q.get("merchant_id") || r.merchant_id === q.get("merchant_id")) &&
        (!q.get("keyword") || r.external_no === q.get("keyword")) &&
        (!q.get("rule_id") || r.rule_id === q.get("rule_id")),
    );
  await context.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname,
      q = url.searchParams;
    if (path.endsWith("/session"))
      return ok(route, {
        user: {
          id: "1",
          role: "admin",
          admin_class: "super",
          name: "Synthetic",
        },
        csrf_token: "synthetic",
      });
    if (path.endsWith("/payment-password"))
      return ok(route, {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      });
    if (req.method() !== "GET") {
      const body = req.postDataJSON();
      writes.push({ path, body });
      if (path.endsWith("/returns") && returnWrite)
        return returnWrite(route, body);
      if (path.includes("/returns/") && path.endsWith("/query")) {
        const original = writes.find((w) => w.path.endsWith("/returns"))?.body;
        expect(body).toEqual({ merchant_id: original.merchant_id });
        return ok(route, {
          id: "301",
          sharing_id: "101",
          merchant_id: original.merchant_id,
          receiver_id: original.receiver_id,
          external_no: original.external_no,
          amount: original.amount,
          state: "succeeded",
        });
      }
      if (path.includes("/orders/") && path.endsWith("/query")) {
        const ext = decodeURIComponent(path.split("/").at(-2)!);
        const row = rows.find(
          (r) => r.external_no === ext && r.merchant_id === body.merchant_id,
        );
        expect(row).toBeTruthy();
        return ok(route, detail(row!));
      }
      return route.abort();
    }
    if (
      path.endsWith("/order-overview") ||
      path.endsWith("/order-overview/statistics")
    ) {
      queries.push(url);
      const matches = filtered(q);
      if (path.endsWith("/statistics"))
        return ok(route, {
          count: String(matches.length),
          share_amount: String(matches.length * 20),
          released_amount: "0",
          success_count: String(matches.length),
          fail_count: "0",
        });
      if (q.get("format") === "csv")
        return route.fulfill({
          status: 200,
          contentType: "text/csv; charset=utf-8",
          headers: {
            "content-disposition": "attachment; filename=sharing.csv",
          },
          body: "\ufeff用户ID,系统订单号\n751,synthetic\n",
        });
      const limit = Number(q.get("limit") || 20),
        offset = Number(q.get("offset") || 0);
      return ok(route, {
        items: matches.slice(offset, offset + limit),
        total: matches.length,
        limit,
      });
    }
    if (path.includes("/orders/")) {
      const ext = decodeURIComponent(path.split("/").at(-1)!);
      const row = rows.find(
        (r) => r.external_no === ext && r.merchant_id === q.get("merchant_id"),
      );
      expect(row).toBeTruthy();
      return ok(route, detail(row!));
    }
    if (path.endsWith("/channels"))
      return ok(route, {
        merchant_id: q.get("merchant_id"),
        available: false,
        reason: "本地没有实时通道",
        channels: [],
      });
    if (path.endsWith("/orders"))
      return ok(route, {
        items: rows
          .filter((r) => r.merchant_id === q.get("merchant_id"))
          .slice(0, 20)
          .map(detail),
        next_cursor: "0",
      });
    if (path.endsWith("/returns"))
      return ok(route, { items: [], next_cursor: "0" });
    return ok(route, { items: [], total: 0 });
  });
  return { queries, writes };
}

for (const width of [375, 1280])
  test(`overview applied paging, export and row operation context at ${width}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const { queries, writes } = await fixture(page.context());
    await page.goto("/admin/profitsharing/order");
    await expect(page.locator(".sharing-orders tbody tr")).toHaveCount(20);
    await expect(
      page
        .locator(".sharing-orders tbody tr")
        .first()
        .locator("td")
        .filter({ hasText: "2026-11-01 09:02:03" }),
    ).toHaveCSS("white-space", "nowrap");
    await expect(page.locator(".sharing-orders tbody")).toContainText(
      "9007199254740993",
    );
    await page
      .getByRole("textbox", { name: "商户ID", exact: true })
      .fill("751");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.locator(".sharing-orders tbody tr")).toHaveCount(20);
    await page
      .getByRole("textbox", { name: "商户ID", exact: true })
      .fill("752");
    await page
      .getByRole("textbox", { name: "搜索内容", exact: true })
      .fill("unapplied-draft");
    await page
      .locator(".sharing-orders .pa-pager")
      .getByRole("button", { name: "2", exact: true })
      .click();
    await expect(page.locator(".sharing-orders tbody tr")).toHaveCount(10);
    expect(queries.at(-1)?.searchParams.get("merchant_id")).toBe("751");
    expect(queries.at(-1)?.searchParams.has("keyword")).toBe(false);
    await page.getByRole("button", { name: "统计", exact: true }).click();
    await expect(page.locator(".sharing-stats")).toContainText("30");
    const downloading = page.waitForEvent("download");
    await page.getByRole("button", { name: "导出", exact: true }).click();
    await downloading;
    expect(queries.at(-1)?.searchParams.get("merchant_id")).toBe("751");
    expect(queries.at(-1)?.searchParams.has("keyword")).toBe(false);
    await page
      .locator(".sharing-orders tbody tr")
      .first()
      .getByRole("button", { name: "查询结果", exact: true })
      .click();
    await expect
      .poll(() => writes.filter((w) => w.path.endsWith("/query")).length)
      .toBe(1);
    expect(writes[0].body).toEqual({ merchant_id: "751" });
    await page.getByRole("button", { name: "重置", exact: true }).click();
    await expect(page.locator(".sharing-orders tbody tr")).toHaveCount(20);
    const trigger = page
      .locator(".sharing-orders tbody tr")
      .nth(1)
      .getByRole("button", { name: "操作台", exact: true });
    await trigger.click();
    const panel = page.locator('section[aria-label="渠道分账记录"]');
    await expect(
      panel.getByRole("textbox", { name: "商户 ID", exact: true }),
    ).toHaveValue("752");
    await expect(
      panel.getByRole("textbox", { name: "商户 ID", exact: true }),
    ).toBeDisabled();
    await expect(panel).toContainText("same-number");
    await page.screenshot({
      path: testInfo.outputPath(`sharing-overview-${width}.png`),
      fullPage: true,
    });
    await page.getByRole("button", { name: "关闭操作台", exact: true }).click();
    await expect(trigger).toBeFocused();
  });

test("an unknown inline return survives reload and only checks the original merchant request", async ({
  page,
}) => {
  const { writes } = await fixture(page.context(), async (route) =>
    route.abort("failed"),
  );
  await page.goto("/admin/profitsharing/order");
  const first = page.locator(".sharing-orders tbody tr").first();
  await first.getByRole("button", { name: "分账回退", exact: true }).click();
  let modal = page.getByRole("dialog", { name: "分账回退", exact: true });
  await modal
    .getByRole("textbox", { name: "回退金额（元）", exact: true })
    .fill("0.10");
  await modal.getByRole("button", { name: "下一步", exact: true }).click();
  await modal.getByRole("button", { name: "确认回退", exact: true }).click();
  await expect(modal).toContainText("结果尚未核实");
  const original = writes[0].body;
  expect(original.merchant_id).toBe("751");
  await page.reload();
  await page
    .locator(".sharing-orders tbody tr")
    .first()
    .getByRole("button", { name: "分账回退", exact: true })
    .click();
  modal = page.getByRole("dialog", { name: "分账回退", exact: true });
  await expect(modal).toContainText(original.external_no);
  await expect(
    modal.getByRole("textbox", { name: "回退金额（元）", exact: true }),
  ).toBeDisabled();
  await modal
    .getByRole("button", { name: "查询回退结果", exact: true })
    .click();
  await expect(modal).toContainText("成功");
  expect(writes.filter((w) => w.path.endsWith("/returns"))).toHaveLength(1);
  expect(writes.at(-1)?.path).toContain(original.external_no);
  const data = await page.evaluate(() =>
    Object.entries(localStorage)
      .filter(([k]) => k.startsWith("goepay.sharing-return.v1."))
      .map(([, v]) => JSON.parse(v)),
  );
  expect(data).toHaveLength(1);
  expect(data[0].state).toBe("succeeded");
  expect(JSON.stringify(data)).not.toContain("csrf");
});

test("two prepared tabs cannot dispatch two different return numbers for the same original share", async ({
  page,
}) => {
  let release!: () => void, sent: any;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  const { writes } = await fixture(page.context(), async (route, body) => {
    sent = body;
    await held;
    await route.fulfill({
      json: {
        code: 0,
        data: {
          id: "301",
          sharing_id: "101",
          merchant_id: body.merchant_id,
          receiver_id: body.receiver_id,
          external_no: body.external_no,
          amount: body.amount,
          state: "pending",
        },
      },
    });
  });
  const second = await page.context().newPage();
  for (const tab of [page, second]) {
    await tab.goto("/admin/profitsharing/order");
    await tab
      .locator(".sharing-orders tbody tr")
      .first()
      .getByRole("button", { name: "分账回退", exact: true })
      .click();
    await tab
      .getByRole("dialog", { name: "分账回退", exact: true })
      .getByRole("textbox", { name: "回退金额（元）", exact: true })
      .fill("0.10");
  }
  const firstModal = page.getByRole("dialog", {
    name: "分账回退",
    exact: true,
  });
  await firstModal.getByRole("button", { name: "下一步", exact: true }).click();
  await firstModal
    .getByRole("button", { name: "确认回退", exact: true })
    .click();
  await expect.poll(() => !!sent).toBe(true);
  const secondModal = second.getByRole("dialog", {
    name: "分账回退",
    exact: true,
  });
  await secondModal
    .getByRole("button", { name: "下一步", exact: true })
    .click();
  await secondModal
    .getByRole("button", { name: "确认回退", exact: true })
    .click();
  await expect(secondModal).toContainText(sent.external_no);
  await expect(
    secondModal.getByRole("textbox", { name: "回退金额（元）", exact: true }),
  ).toBeDisabled();
  expect(writes.filter((w) => w.path.endsWith("/returns"))).toHaveLength(1);
  release();
  await expect(firstModal).toContainText("处理中");
  await second.close();
});

for (const width of [375, 1280])
  test(`selected sharing queries preserve two merchants with the same business number at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const { queries, writes } = await fixture(page.context());
    await page.goto("/admin/profitsharing/order");
    const checks = page.locator('.sharing-orders tbody input[type="checkbox"]');
    await checks.nth(0).check();
    await checks.nth(1).check();
    const trigger = page.locator('[data-action="batch"]');
    await trigger.click();
    let dialog = page.getByRole("dialog", {
      name: "批量查询分账结果",
      exact: true,
    });
    await expect(dialog).toContainText("商户 751");
    await expect(dialog).toContainText("商户 752");
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(trigger).toBeFocused();
    expect(writes).toHaveLength(0);
    await trigger.click();
    dialog = page.getByRole("dialog", {
      name: "批量查询分账结果",
      exact: true,
    });
    await dialog.getByRole("button", { name: "确认查询", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "已核对 2 / 2 条分账记录" }),
    ).toBeVisible();
    expect(writes.map((w) => w.body.merchant_id)).toEqual(["751", "752"]);
    expect(writes.every((w) => w.path.endsWith("/same-number/query"))).toBe(
      true,
    );
    expect(
      queries.filter((q) => q.pathname.endsWith("/order-overview")),
    ).toHaveLength(2);
    await expect(dialog).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 375) {
      await page
        .locator('.sharing-orders thead input[type="checkbox"]')
        .check();
      await trigger.click();
      const list = page
        .getByRole("dialog", { name: "批量查询分账结果", exact: true })
        .locator(".batch-review-rows");
      await list.focus();
      await expect(list).toBeFocused();
      await list.press("End");
      await expect
        .poll(() => list.evaluate((el) => el.scrollTop))
        .toBeGreaterThan(0);
      await page
        .getByRole("dialog", { name: "批量查询分账结果", exact: true })
        .getByRole("button", { name: "取消", exact: true })
        .click();
      expect(writes).toHaveLength(2);
    }
  });
