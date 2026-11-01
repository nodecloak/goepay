import { test, expect } from "@playwright/test";
for (const width of [1280, 375])
  test(`original merchant manual withdrawal binds exact target and explicit quote at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const id = "9223372036854775807",
      reads: URL[] = [],
      posts: { path: string; body: Record<string, unknown> }[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        p = u.pathname;
      if (req.method() !== "GET")
        posts.push({ path: p, body: req.postDataJSON() });
      reads.push(u);
      let data: unknown = { items: [], total: 0 };
      let status = 200;
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "operator" },
          csrf_token: "manual159",
        };
      else if (p.endsWith("/merchants/schema"))
        data = {
          title: "商户管理",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: true, update: true, delete: false },
        };
      else if (p.endsWith("/resources/merchants"))
        data = { items: [{ id, name: "目标商户159" }], total: 1 };
      else if (p.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (p.endsWith("/workflows/settlement-recipient/allowed-types")) {
        expect(u.searchParams.get("merchant_id")).toBe(id);
        data = { settlement: ["alipay", "wxpay", "bank"], can_write: true };
      } else if (p.endsWith("/workflows/settlement-recipient")) {
        expect(u.searchParams.get("merchant_id")).toBe(id);
        data = {
          ...(req.method() === "GET" ? { can_write: true } : {}),
          state: "configured",
          config_id: "159",
          type: "alipay",
          app_id: "",
          account_masked: "***0159",
          name_masked: "***",
        };
      } else if (p.endsWith("/workflows/funds-availability")) {
        expect(u.searchParams.get("merchant_id")).toBe(id);
        data = {
          available_money: "10.00",
          ledger_available_money: "10.00",
          today_blocked_money: "0.00",
          settle_type: 0,
        };
      } else if (p.endsWith("/workflows/settlement-policy")) {
        expect(u.searchParams.get("merchant_id")).toBe(id);
        expect(u.searchParams.get("amount")).toBe("1.23");
        data = {
          can_apply: true,
          external_payout: false,
          terms: {
            gross_amount: "123",
            fee_amount: "3",
            net_amount: "120",
            retained_cents: "0",
          },
        };
      } else if (
        p.endsWith("/workflows/settlements") &&
        req.method() === "POST"
      ) {
        expect(req.postDataJSON()).toMatchObject({
          merchant_id: id,
          amount: "1.23",
          recipient_config_id: "159",
          expected_fee_cents: "3",
          expected_net_cents: "120",
        });
        data = {
          id: "159",
          kind: "settlements",
          merchant_id: id,
          external_no: req.postDataJSON().external_no,
          status: "pending",
          amount: "1.23",
          amount_cents: "123",
          external_payout: false,
          created_at: "2026-10-30T00:00:00Z",
          settlement: {
            gross_amount: "123",
            fee_amount: "3",
            net_amount: "120",
            retained_cents: "0",
          },
          settlement_recipient: {
            state: "recorded",
            config_id: "159",
            type: "alipay",
            app_id: "",
            account_masked: "***0159",
            name_masked: "***",
          },
          settlement_funding: {
            state: "reserved",
            account_id: "7",
            gross_amount: "123",
            reserved_amount: "123",
          },
        };
        status = 201;
      }
      return route.fulfill({ status, json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/merchant-list");
    const opener = page.getByRole("button", {
      name: "商户关联记录 " + id,
      exact: true,
    });
    await opener.click();
    await page.getByRole("menuitem", { name: "手动提现", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "手动提现", exact: true });
    await expect(dialog).toBeVisible();
    const merchant = dialog.getByLabel("付款商户 ID", { exact: true });
    await expect(merchant).toHaveValue(id);
    await expect(merchant).toHaveAttribute("readonly", "");
    await expect(
      dialog.getByTestId("settlement-recipient-current"),
    ).toContainText("版本 159");
    expect(posts).toEqual([]);
    await expect(
      dialog.getByRole("button", { name: "确认提交", exact: true }),
    ).toBeDisabled();
    await dialog.getByLabel("金额（元）", { exact: true }).fill("1.23");
    await dialog
      .getByRole("button", { name: "查询可出账金额", exact: true })
      .click();
    await expect(
      dialog.getByRole("button", { name: "确认提交", exact: true }),
    ).toBeEnabled();
    await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
    await expect(dialog).toContainText("预计结算净额：1.20 元");
    await expect(
      dialog.getByRole("link", { name: "返回提现记录", exact: true }),
    ).toHaveAttribute("href", "/admin/settle/list?uid=" + id);
    await page.screenshot({
      path: test.info().outputPath(`manual-withdrawal-${width}.png`),
      fullPage: true,
    });
    expect(posts).toEqual([]);
    await dialog
      .getByRole("button", { name: "按已核对费用提交", exact: true })
      .click();
    await expect(dialog).toContainText("申请已提交");
    expect(
      posts.filter((p) => p.path.endsWith("/workflows/settlements")),
    ).toHaveLength(1);
    expect(
      posts.filter((p) => !p.path.endsWith("/workflows/settlements")),
    ).toEqual([]);
    await dialog
      .getByRole("button", { name: "关闭手动提现", exact: true })
      .click();
    await expect(opener).toBeFocused();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
test("readonly merchant permissions cannot expose manual withdrawal", async ({
  page,
}) => {
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    const data = p.endsWith("/session")
      ? {
          user: { id: "1", role: "admin", admin_class: "operator" },
          csrf_token: "manual159",
        }
      : p.endsWith("/merchants/schema")
        ? {
            title: "商户管理",
            fields: [{ name: "name", label: "名称", type: "string" }],
            permissions: { create: false, update: false, delete: false },
          }
        : { items: [{ id: "159", name: "只读商户" }], total: 1 };
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/merchant/merchant-list");
  await page
    .getByRole("button", { name: "商户关联记录 159", exact: true })
    .click();
  await expect(
    page.getByRole("menuitem", { name: "手动提现", exact: true }),
  ).toHaveCount(0);
});

test("cancelling manual withdrawal clears target amount and never submits or reads an unrelated merchant", async ({
  page,
}) => {
  const ids = ["15901", "15902"],
    recipientReads: string[] = [],
    writes: string[] = [];
  await page.route("**/console/v1/**", (route) => {
    const r = route.request(),
      u = new URL(r.url()),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (r.method() !== "GET") writes.push(p);
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "super" },
        csrf_token: "cancel159",
      };
    else if (p.endsWith("/merchants/schema"))
      data = {
        title: "商户管理",
        fields: [{ name: "name", label: "名称", type: "string" }],
        permissions: { create: true, update: true, delete: false },
      };
    else if (p.endsWith("/resources/merchants"))
      data = {
        items: ids.map((id) => ({ id, name: "目标商户" + id })),
        total: 2,
      };
    else if (p.endsWith("/workflows/settlement-recipient")) {
      const mid = u.searchParams.get("merchant_id")!;
      expect(ids).toContain(mid);
      recipientReads.push(mid);
      data = {
        ...(r.method() === "GET" ? { can_write: true } : {}),
        state: "configured",
        config_id: mid,
        type: "alipay",
        app_id: "",
        account_masked: "***" + mid.slice(-4),
        name_masked: "***",
      };
    }
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/merchant/merchant-list");
  for (const id of ids) {
    const opener = page.getByRole("button", {
      name: "商户关联记录 " + id,
      exact: true,
    });
    await opener.click();
    await page.getByRole("menuitem", { name: "手动提现", exact: true }).click();
    const d = page.getByRole("dialog", { name: "手动提现", exact: true });
    await expect(d.getByLabel("付款商户 ID", { exact: true })).toHaveValue(id);
    await expect(d.getByLabel("金额（元）", { exact: true })).toHaveValue("");
    await expect(d.getByTestId("settlement-recipient-current")).toContainText(
      "版本 " + id,
    );
    await d.getByLabel("金额（元）", { exact: true }).fill("9.99");
    await page.keyboard.press("Escape");
    await expect(d).toHaveCount(0);
    await expect(opener).toBeFocused();
  }
  expect(recipientReads).toEqual(ids);
  expect(writes).toEqual([]);
});
