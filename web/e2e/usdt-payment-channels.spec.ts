import { expect, test, type Page } from "@playwright/test";

const channel = {
  asset: "USDT",
  id: "42",
  name: "TRON synthetic",
  pay_type: "usdt",
  plugin: "usdt_tron",
  configured: true,
  network: "tron",
  revision: "3",
  enabled: false,
  archived: false,
  runtime_enabled: false,
  fee_bps: 75,
  min_amount: "0.1",
  max_amount: "100",
  day_limit: "1000",
  created_at: "2026-11-13T00:00:00Z",
  updated_at: "2026-11-13T00:00:00Z",
};
test.beforeEach(async ({ context, baseURL }) => {
  if (
    !baseURL ||
    !["127.0.0.1", "localhost"].includes(new URL(baseURL).hostname)
  )
    throw Error("Local fixture required");
  await context.route("**/*", (route) =>
    ["127.0.0.1", "localhost"].includes(new URL(route.request().url()).hostname)
      ? route.fallback()
      : route.abort(),
  );
});
async function mock(
  page: Page,
  area: "admin" | "merchant",
  options: { channels: boolean; unknown: boolean; archived?: boolean } = {
    channels: true,
    unknown: false,
  },
) {
  let rows = [{ ...channel, archived: options.archived ?? false }];
  const writes: { path: string; body: any }[] = [],
    reads: string[] = [],
    errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname.replace(`/console/v1/${area}`, "");
    const ok = (data: unknown) => route.fulfill({ json: { code: 0, data } });
    if (req.method() !== "GET") {
      const body = req.postDataJSON();
      if (path.startsWith("/payment-channels/crypto"))
        expect(body).not.toHaveProperty("merchant_ids");
      writes.push({ path, body });
      if (options.unknown) return route.abort();
      if (path === "/payment-channels/crypto") {
        expect(body.enabled).toBe(false);
        expect(body.request_id).toMatch(/^[a-f0-9]{32}$/);
        const row = {
          ...channel,
          ...body,
          id: "43",
          revision: "1",
          plugin: "usdt_" + body.network,
        };
        delete row.expected_revision;
        delete row.request_id;
        rows.push(row);
        return ok(row);
      }
      if (path === "/payment-channels/crypto/42") {
        const row = { ...channel, ...body, revision: "4" };
        delete row.expected_revision;
        rows = [row];
        return ok(row);
      }
      if (path === "/crypto/invoices") {
        expect(body.channel_id).toBe("42");
        expect(body.network).toBe("tron");
        return ok({ checkout_url: "/synthetic-only-checkout" });
      }
      throw Error("Unexpected mocked write: " + path);
    }
    reads.push(path);
    if (path === "/session")
      return ok({
        user: {
          id: "1000",
          name: "Synthetic user",
          role: area,
          ...(area === "admin" ? { admin_class: "super" } : {}),
        },
        csrf_token: "local-only-csrf",
      });
    if (path === "/security/payment-password")
      return ok({
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      });
    if (path === "/payment-channels/templates")
      return ok({ items: [], total: 0 });
    if (path === "/payment-channels")
      return ok({ items: rows, total: rows.length, page: 1, size: 20 });
    if (path === "/crypto/config")
      return ok({
        configured: true,
        enabled: true,
        simulation: true,
        networks: [{ network: "tron", enabled: true, fee_bps: 100 }],
      });
    if (path === "/crypto/channels")
      return ok({
        items: options.channels
          ? [
              {
                id: "42",
                name: "TRON synthetic",
                network: "tron",
                fee_bps: 75,
                min_amount: "0.1",
                max_amount: "100",
                day_limit: "1000",
              },
            ]
          : [],
      });
    if (path === "/crypto/invoices")
      return ok([
        {
          id: "synthetic-original",
          merchant_id: "1000",
          out_trade_no: "original-order",
          network: "tron",
          amount: "10",
          received: "0",
          fee: "0.075",
          state: "pending",
          address: "synthetic-address",
          expires_at: "2026-11-14T00:00:00Z",
          channel_id: "42",
          channel_revision: "3",
        },
      ]);
    if (path.startsWith("/crypto/")) return ok([]);
    return ok({ enabled: false });
  });
  return { writes, reads, errors };
}
for (const width of [375, 1440]) {
  test(`USDT channels edit and cancel fit ${width} with mainnet closed`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 960 });
    const state = await mock(page, "admin");
    await page.goto("/admin/payapi/pay-channel");
    const row = page.locator("tbody tr").filter({ hasText: "TRON synthetic" });
    await expect(row).toContainText("主网收款关闭");
    await expect(
      row.getByRole("button", { name: "测试", exact: true }),
    ).toHaveCount(0);
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const editor = page.getByRole("dialog", { name: "编辑 USDT 通道" });
    await expect(editor).toBeVisible();
    await expect(editor.locator("select[name=network]")).toBeDisabled();
    await editor.locator("input[name=fee]").fill("0.5");
    const sizes = await editor.evaluate((el) => ({
      width: el.getBoundingClientRect().width,
      scroll: el.scrollWidth,
      client: el.clientWidth,
      select: el.querySelector("select")!.getBoundingClientRect().width,
    }));
    expect(sizes.width).toBeLessThanOrEqual(width + 1);
    expect(sizes.scroll).toBeLessThanOrEqual(sizes.client + 1);
    expect(sizes.select).toBeLessThanOrEqual(400);
    await editor.getByRole("button", { name: "取消", exact: true }).click();
    await expect(editor).toHaveCount(0);
    expect(state.writes).toHaveLength(0);
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(editor.locator("input[name=fee]")).toHaveValue("0.75");
    await editor.locator("input[name=fee]").fill("0.5");
    await editor.getByRole("button", { name: "保存", exact: true }).click();
    await expect(editor).toHaveCount(0);
    expect(state.writes).toHaveLength(1);
    expect(state.writes[0]!.body).toMatchObject({
      fee_bps: 50,
      expected_revision: "3",
      enabled: false,
    });
    await expect(row).toContainText("主网收款关闭");
    expect(state.errors).toEqual([]);
  });
}
test("creates an explicitly closed Ethereum channel in the unified catalog", async ({
  page,
}) => {
  const state = await mock(page, "admin");
  await page.goto("/admin/payapi/pay-channel");
  await page
    .getByRole("button", { name: "新增 USDT 通道", exact: true })
    .click();
  const editor = page.getByRole("dialog", { name: "新增 USDT 通道" });
  await editor.locator("input[name=name]").fill("Ethereum synthetic");
  await editor.locator("select[name=network]").selectOption("ethereum");
  await editor.locator("input[name=fee]").fill("0.8");
  await editor.locator("input[name=min_amount]").fill("0.000000");
  await editor.locator("input[name=max_amount]").fill("0.00");
  await editor.locator("input[name=day_limit]").fill("0.0");
  await expect(editor.locator("textarea[name=merchants]")).toHaveCount(0);
  await expect(editor).toContainText("新增通道不会自动向所有商户开放");
  await editor.getByRole("button", { name: "保存", exact: true }).click();
  await expect(editor).toHaveCount(0);
  expect(state.writes).toHaveLength(1);
  expect(state.writes[0]!.body).toMatchObject({
    min_amount: "0",
    max_amount: "0",
    day_limit: "0",
  });
  await expect(
    page.locator("tbody tr").filter({ hasText: "Ethereum synthetic" }),
  ).toContainText("主网收款关闭");
  expect(state.errors).toEqual([]);
});
test("archived channel details stay read-only and close without a write", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 960 });
  const state = await mock(page, "admin", {
    channels: true,
    unknown: false,
    archived: true,
  });
  await page.goto("/admin/payapi/pay-channel");
  const row = page.locator("tbody tr");
  await expect(
    row.getByRole("button", { name: "编辑", exact: true }),
  ).toHaveCount(0);
  await row.getByRole("button", { name: "查看", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "USDT 通道详情" });
  await expect(dialog.locator("input[name=name]")).toBeDisabled();
  await expect(dialog.locator("input[name=archived]")).toBeDisabled();
  await expect(
    dialog.getByRole("button", { name: "保存", exact: true }),
  ).toHaveCount(0);
  await dialog.locator("button[type=submit]").click();
  await expect(dialog).toHaveCount(0);
  expect(state.writes).toHaveLength(0);
  expect(state.errors).toEqual([]);
});
test("locks an unknown configuration save instead of reposting", async ({
  page,
}) => {
  const state = await mock(page, "admin", { channels: true, unknown: true });
  await page.goto("/admin/payapi/pay-channel");
  await page
    .locator("tbody tr")
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  const editor = page.getByRole("dialog");
  await editor.getByRole("button", { name: "保存", exact: true }).click();
  await expect(editor).toContainText("保存结果尚未确认");
  await expect(
    editor.getByRole("button", { name: "保存", exact: true }),
  ).toBeDisabled();
  expect(state.writes).toHaveLength(1);
});
for (const allowed of [true, false]) {
  test(`merchant order asset view enforces granted channel availability ${allowed}`, async ({
    page,
  }) => {
    const state = await mock(page, "merchant", {
      channels: allowed,
      unknown: false,
    });
    await page.goto("/merchant/trade/order?type=usdt&channel_id=42");
    await expect(
      page.getByRole("navigation", { name: "订单资产" }),
    ).toBeVisible();
    await expect(page.locator(".crypto-funds")).toHaveAttribute(
      "data-section",
      "invoices",
    );
    await expect(page.locator(".crypto-table")).toContainText("#42 · 版本 3");
    await page.locator("[data-open-form=invoice]").click();
    const form = page.locator("[data-form=invoice]");
    if (allowed) {
      await expect(form).toContainText("手续费 0.75%");
      await form.locator("input[name=out_trade_no]").fill("mock-invoice");
      await form.locator("input[name=amount]").fill("1.000001");
      await form
        .getByRole("button", { name: "创建收款单", exact: true })
        .click();
      await expect(page.locator("[data-checkout]")).toHaveAttribute(
        "href",
        "/synthetic-only-checkout",
      );
      expect(state.writes).toHaveLength(1);
    } else {
      await expect(form).toContainText("当前没有可用的 USDT 通道");
      await expect(
        form.getByRole("button", { name: "创建收款单", exact: true }),
      ).toBeDisabled();
      expect(state.writes).toHaveLength(0);
    }
    expect(state.reads.some((path) => path === "/orders")).toBe(false);
    expect(state.errors).toEqual([]);
  });
}
