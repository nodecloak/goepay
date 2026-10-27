import { test, expect } from "@playwright/test";
for (const width of [1440, 375])
  test(`merchant167 original conditional menus and protected actions ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const trade = (n: number) => "202610260000000000" + n;
    const p = trade(167),
      paid = trade(168),
      partial = trade(169),
      unknown = trade(170);
    let rows = [
      {
        id: "9007199254740993",
        trade_no: p,
        status: "pending",
        merchant_actions: { query: true },
      },
      {
        id: "9007199254740995",
        trade_no: paid,
        status: "paid",
        merchant_actions: { notify: true, refund: true, print: true },
      },
      {
        id: "9007199254740997",
        trade_no: partial,
        status: "paid",
        refunded_amount: "1000",
        merchant_actions: { refund: true },
      },
      {
        id: "9007199254740999",
        trade_no: unknown,
        status: "pending",
        merchant_actions: {},
      },
    ].map((r) => ({
      merchant_id: "1",
      out_trade_no: "商户原完整单号".repeat(6),
      name: "合成商品167",
      type: "wxpay",
      amount: "10000",
      refunded_amount: "0",
      income_status: "none",
      created_at: "2026-10-26T01:00:00Z",
      ...r,
    }));
    const writes: { path: string; body: unknown }[] = [],
      reads: URL[] = [];
    let detailReads = 0;
    let finishPrint: (() => Promise<void>) | undefined;
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", (route) => {
      const r = route.request(),
        u = new URL(r.url());
      if (!["127.0.0.1", "localhost"].includes(u.hostname))
        return route.abort();
      if (!u.pathname.startsWith("/console/v1/")) return route.continue();
      if (r.method() === "GET") reads.push(u);
      else writes.push({ path: u.pathname, body: r.postDataJSON() });
      let data: unknown = { items: [], total: 0 };
      let status = 200;
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", name: "本地商户167", role: "merchant" },
          csrf_token: "synthetic167",
        };
      else if (u.pathname.endsWith("/orders"))
        data = { items: rows, total: rows.length };
      else {
        const match = u.pathname.match(/\/orders\/([^/]+)(?:\/(.+))?$/);
        if (match) {
          const no = match[1]!,
            action = match[2],
            row = rows.find((v) => v.trade_no === no)!;
          if (!action) {
            detailReads++;
            data = {
              order: row,
              reporting: { refunded_amount: row.refunded_amount },
              merchant_actions: row.merchant_actions,
              can_resend_notification: true,
            };
          } else if (action === "query") {
            rows = rows.map((v) =>
              v.trade_no === no
                ? {
                    ...v,
                    status: "paid",
                    merchant_actions: {
                      notify: true,
                      refund: true,
                      print: true,
                    },
                  }
                : v,
            );
            data = rows.find((v) => v.trade_no === no);
          } else if (action === "receipt")
            data = {
              trade_no: no,
              merchant_name: "合成商户",
              name: "合成小票",
              amount: "10000",
              type: "wxpay",
              paid_at: "2026-10-26T01:00:00Z",
              remark: "",
            };
          else if (action === "print-jobs") {
            if (r.method() === "POST")
              return new Promise<void>((resolve) => {
                finishPrint = async () => {
                  await route.fulfill({
                    status: 202,
                    contentType: "application/json",
                    body: JSON.stringify({
                      code: 0,
                      data: {
                        id: "9007199254740999",
                        trade_no: no,
                        external_no: r.postDataJSON().external_no,
                        request_no: "synthetic167-print",
                        state: "queued",
                        provider: "local_fake",
                        copies: 1,
                        device_masked: "***167",
                      },
                    }),
                  });
                  resolve();
                };
              });
            data = { items: [], next_cursor: "" };
          } else if (action === "resend-notification") {
            status = 202;
            data = { job_id: "167", state: "pending" };
          }
        }
      }
      return route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/merchant/trade/order");
    const trigger = (no: string) =>
      page.getByRole("button", { name: "订单操作 " + no, exact: true });
    await expect(trigger(p)).toBeVisible();
    expect(detailReads).toBe(0);
    const open = async (no: string) => {
      await trigger(no).click();
      return page.getByRole("menu", { name: "订单操作 " + no, exact: true });
    };
    let menu = await open(unknown);
    await expect(menu.getByRole("menuitem")).toHaveText([
      "查看详情",
      "资金明细",
    ]);
    await page.keyboard.press("Escape");
    await expect(trigger(unknown)).toBeFocused();
    menu = await open(p);
    await expect(menu).toContainText("手动补单");
    await menu.getByRole("menuitem", { name: "手动补单", exact: true }).click();
    let dialog = page.getByRole("dialog", { name: "手动补单", exact: true });
    await expect(
      dialog.getByRole("button", { name: "确认查询原订单", exact: true }),
    ).toBeVisible();
    expect(writes).toHaveLength(0);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(trigger(p)).toBeFocused();
    expect(writes).toHaveLength(0);
    menu = await open(p);
    await menu.getByRole("menuitem", { name: "手动补单", exact: true }).click();
    await dialog
      .getByRole("button", { name: "确认查询原订单", exact: true })
      .click();
    await expect(dialog).toContainText("原通道已核实支付成功");
    expect(writes.filter((v) => v.path.endsWith("/query"))).toHaveLength(1);
    await page.keyboard.press("Escape");
    await expect(trigger(p)).toBeFocused();
    menu = await open(paid);
    await expect(menu.getByRole("menuitem")).toHaveText([
      "查看详情",
      "资金明细",
      "重新通知",
      "退款",
      "打印小票",
    ]);
    await page.screenshot({
      path: info.outputPath(`merchant167-${width}-menu.png`),
      fullPage: true,
    });
    await menu.getByRole("menuitem", { name: "重新通知", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "重新通知", exact: true });
    await expect(dialog).toContainText("核对商户通知重发");
    await dialog
      .getByRole("button", { name: "取消通知请求", exact: true })
      .click();
    await page.keyboard.press("Escape");
    expect(
      writes.filter((v) => v.path.endsWith("/resend-notification")),
    ).toHaveLength(0);
    await expect(trigger(paid)).toBeFocused();
    menu = await open(paid);
    await menu.getByRole("menuitem", { name: "打印小票", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "打印小票", exact: true });
    await dialog
      .getByRole("button", { name: "查看或刷新打印收据", exact: true })
      .click();
    await expect(dialog).toContainText("只显示已核实支付收据");
    expect(writes.filter((v) => v.path.endsWith("/print-jobs"))).toHaveLength(
      0,
    );
    await page.screenshot({
      path: info.outputPath(`merchant167-${width}-print.png`),
      fullPage: true,
    });
    await dialog
      .getByRole("button", { name: "创建手工打印请求", exact: true })
      .click();
    await dialog
      .getByRole("button", { name: "确认创建打印请求", exact: true })
      .click();
    await expect
      .poll(() => writes.filter((v) => v.path.endsWith("/print-jobs")).length)
      .toBe(1);
    await expect(
      dialog.getByRole("button", { name: "关闭", exact: true }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();
    await finishPrint!();
    await expect(dialog).toContainText("尚未发送");
    await page.keyboard.press("Escape");
    await expect(trigger(paid)).toBeFocused();
    menu = await open(paid);
    await menu.getByRole("menuitem", { name: "退款", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "申请退款", exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel("退款金额（元）")).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(
      dialog.getByRole("button", { name: "确认申请退款", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(dialog.getByLabel("退款金额（元）")).toBeFocused();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(trigger(paid)).toBeFocused();
    expect(writes.filter((v) => v.path.endsWith("/refund"))).toHaveLength(0);
    menu = await open(partial);
    await menu.getByRole("menuitem", { name: "退款记录", exact: true }).click();
    await expect(page).toHaveURL(
      new RegExp("/merchant/trade/refund\\?trade_no=" + partial),
    );
    await expect
      .poll(() =>
        reads.some(
          (u) =>
            u.pathname.endsWith("/refunds") &&
            u.searchParams.get("q") === partial &&
            u.searchParams.get("search_field") === "trade_no",
        ),
      )
      .toBe(true);
    await page.goto("/merchant/trade/order");
    menu = await open(paid);
    await menu.getByRole("menuitem", { name: "资金明细", exact: true }).click();
    await expect
      .poll(() =>
        reads.some(
          (u) =>
            u.pathname.endsWith("/ledger") &&
            u.searchParams.get("q") === paid &&
            u.searchParams.get("search_field") === "trade_no",
        ),
      )
      .toBe(true);
    await expect(page.getByRole("alert")).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
    expect(errors).toEqual([]);
  });
