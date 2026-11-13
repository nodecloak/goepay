import { expect, test, type Page } from "@playwright/test";
import { createHash } from "node:crypto";

type Role = "admin" | "merchant" | "delegated" | "operator";
type Mode = "disabled" | "paused" | "enabled" | "config-error" | "list-error";
const historyAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE";
type Section =
  | "overview"
  | "invoices"
  | "settlements"
  | "refunds"
  | "recipients"
  | "operations"
  | "reconciliation";
const businessSections = [
  {
    section: "invoices",
    action: "创建收款单",
    form: "invoice",
    history: "synthetic-usdt-order-history",
  },
  {
    section: "settlements",
    action: "申请原链结算",
    form: "withdrawal",
    history: "synthetic-usdt-settlement-history",
  },
  {
    section: "refunds",
    action: "申请原链退币",
    form: "refund",
    history: "synthetic-usdt-refund-history",
  },
  {
    section: "recipients",
    action: "申请绑定地址",
    form: "recipient",
    history: historyAddress,
  },
] as const;
const layouts = [
  { width: 375, theme: "dark" },
  { width: 1440, theme: "light" },
] as const;

test.beforeEach(async ({ context, baseURL }) => {
  const localHosts = new Set(["localhost", "127.0.0.1"]);
  if (!baseURL || !localHosts.has(new URL(baseURL).hostname))
    throw Error("USDT mock checks require a loopback fixture URL");
  await context.route("**/*", async (route) => {
    if (!localHosts.has(new URL(route.request().url()).hostname))
      return route.abort();
    return route.fallback();
  });
});

async function mockConsole(page: Page, role: Role, initialMode: Mode) {
  const area = role === "admin" || role === "operator" ? "admin" : "merchant";
  let mode = initialMode;
  const writes: string[] = [],
    cryptoReads: string[] = [],
    lookupReads: string[] = [],
    errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (request.method() !== "GET") {
      writes.push(`${request.method()} ${path}`);
      return route.abort();
    }
    const ok = (data: unknown) => route.fulfill({ json: { code: 0, data } });
    if (path.endsWith("/session"))
      return ok({
        user: {
          id: area === "admin" ? "1" : "1000",
          name: "Synthetic USDT review",
          role: area,
          ...(area === "admin"
            ? {
                admin_class: role === "admin" ? "super" : "operator",
                ...(role === "operator" ? { permissions: [] } : {}),
              }
            : {}),
        },
        csrf_token: "synthetic-usdt-disabled-review",
        ...(role === "delegated"
          ? { delegated_by: { id: "1", name: "Synthetic administrator" } }
          : {}),
      });
    if (path.endsWith("/security/payment-password"))
      return ok({
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      });
    if (!path.includes("/crypto/")) return ok({ enabled: false });
    cryptoReads.push(path);
    if (path.endsWith("/crypto/withdrawals/by-request")) {
      lookupReads.push(request.url());
      return route.fulfill({
        status: 404,
        json: { code: -1, message: "not found" },
      });
    }
    if (
      (mode === "config-error" && path.endsWith("/crypto/config")) ||
      (mode === "list-error" && path.endsWith("/crypto/invoices"))
    )
      return route.fulfill({
        status: 503,
        json: { code: -1, message: "USDT 记录暂时无法读取，请刷新重试" },
      });
    if (path.endsWith("/crypto/config"))
      return ok({
        configured: mode !== "disabled",
        enabled: mode === "enabled" || mode === "list-error",
        networks:
          mode === "disabled"
            ? []
            : [
                {
                  network: "tron",
                  enabled: mode !== "paused",
                  fee_bps: 50,
                },
              ],
      });
    if (path.endsWith("/crypto/balances"))
      return ok([
        { network: "tron", available: "7.123456", reserved: "0.000001" },
      ]);
    if (path.endsWith("/crypto/invoices"))
      return ok([
        {
          id: "synthetic-invoice-history",
          merchant_id: 1000,
          out_trade_no: "synthetic-usdt-order-history",
          network: "tron",
          amount: "2.000000",
          received: "2.000000",
          fee: "0.010000",
          state: "paid",
          address: historyAddress,
          expires_at: "2026-01-01T00:00:00Z",
          checkout_url: "/crypto/checkout/synthetic-history?token=synthetic",
        },
      ]);
    if (path.endsWith("/crypto/withdrawals"))
      return ok([
        {
          id: "synthetic-withdrawal-history",
          merchant_id: 1000,
          request_id: "synthetic-usdt-settlement-history",
          network: "tron",
          address: historyAddress,
          amount: "1.000000",
          state: "requested",
          kind: "settlement",
          tx_id: "",
        },
        {
          id: "synthetic-refund-history",
          merchant_id: 1000,
          request_id: "synthetic-usdt-refund-history",
          network: "tron",
          address: historyAddress,
          amount: "0.500000",
          state: "requested",
          kind: "refund",
          tx_id: "",
        },
      ]);
    if (path.endsWith("/crypto/recipients"))
      return ok([
        {
          id: "synthetic-recipient-history",
          merchant_id: "1000",
          network: "tron",
          address: historyAddress,
          state: "approved",
          usable_at: "2026-01-01T00:00:00Z",
        },
      ]);
    if (path.endsWith("/crypto/reconciliation"))
      return ok({
        imbalanced_journals: 0,
        balance_mismatches: 0,
        negative_balances: 0,
      });
    return ok([]);
  });
  return {
    area,
    basePath: `/${area}/${area === "admin" ? "system" : "account"}/crypto`,
    writes,
    cryptoReads,
    lookupReads,
    errors,
    setMode(next: Mode) {
      mode = next;
    },
    async open(section: Section = "overview") {
      await page.goto(
        `/${area}/${area === "admin" ? "system" : "account"}/crypto${section === "overview" ? "" : "/" + section}`,
      );
    },
  };
}

async function useLayout(page: Page, layout: (typeof layouts)[number]) {
  await page.setViewportSize({ width: layout.width, height: 1000 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.addInitScript(
    (theme) => localStorage.setItem("goepay-console-theme", theme),
    layout.theme,
  );
}

async function expectNoFundsForms(page: Page) {
  const funds = page.locator(".crypto-funds");
  await expect(funds.locator("form[data-form]")).toHaveCount(0);
  await expect(funds.locator('input[type="password"]')).toHaveCount(0);
  await expect(funds.locator("a[data-checkout]")).toHaveCount(0);
}

async function expectNoOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}

async function expectSection(page: Page, section: Section) {
  await expect(page.locator(".crypto-funds")).toHaveAttribute(
    "data-section",
    section,
  );
  expect(new URL(page.url()).hash).toBe("");
  expect(new URL(page.url()).pathname).toMatch(
    section === "overview" ? /\/crypto$/ : new RegExp(`/crypto/${section}$`),
  );
}

for (const role of ["admin", "merchant"] as const) {
  for (const layout of layouts) {
    test(`${role} disabled USDT is closed and can refresh at ${layout.width} ${layout.theme} without writes`, async ({
      page,
    }) => {
      await useLayout(page, layout);
      const fixture = await mockConsole(page, role, "disabled");
      await fixture.open();
      await expect(page.getByTestId("crypto-service-disabled")).toContainText(
        "USDT 收款与结算尚未启用",
      );
      await expectNoFundsForms(page);
      await expect(
        page.getByRole("button", { name: "查询账本对账结果", exact: true }),
      ).toHaveCount(0);
      await expect(page.locator(".crypto-funds")).not.toContainText(
        "资金结果尚无法确认",
      );
      await expectNoOverflow(page);
      expect(fixture.cryptoReads).toEqual([
        `/console/v1/${fixture.area}/crypto/config`,
      ]);
      await page.getByRole("button", { name: "刷新记录", exact: true }).click();
      await expect(page.getByTestId("crypto-service-disabled")).toBeVisible();
      expect(fixture.cryptoReads).toEqual([
        `/console/v1/${fixture.area}/crypto/config`,
        `/console/v1/${fixture.area}/crypto/config`,
      ]);

      fixture.setMode("enabled");
      await page.getByRole("button", { name: "刷新记录", exact: true }).click();
      await expect(page.locator(".crypto-funds")).toContainText("7.123456");
      await expect(page.getByTestId("crypto-service-disabled")).toHaveCount(0);
      await expectSection(page, "overview");
      await expectNoFundsForms(page);
      await expect(page.locator(".crypto-table")).toHaveCount(0);
      const disabledSectionReads = fixture.cryptoReads.length;
      for (const section of [
        ...businessSections.map((item) => item.section),
        ...(role === "admin"
          ? (["operations", "reconciliation"] as const)
          : []),
      ]) {
        fixture.setMode("disabled");
        await fixture.open(section);
        await expectSection(page, section);
        await expect(page.getByTestId("crypto-service-disabled")).toBeVisible();
        await expectNoFundsForms(page);
        await expect(page.locator(".crypto-table")).toHaveCount(0);
      }
      expect(
        fixture.cryptoReads
          .slice(disabledSectionReads)
          .every((path) => path.endsWith("/crypto/config")),
      ).toBe(true);
      await expectNoOverflow(page);
      expect(fixture.writes).toEqual([]);
      expect(fixture.errors).toEqual([]);
    });
  }
}

for (const role of ["admin", "merchant"] as const) {
  for (const layout of layouts) {
    test(`${role} USDT business routes isolate records and reveal one requested form at ${layout.width} ${layout.theme}`, async ({
      page,
    }, testInfo) => {
      await useLayout(page, layout);
      const fixture = await mockConsole(page, role, "enabled");
      await fixture.open();
      const funds = page.locator(".crypto-funds");
      await expectSection(page, "overview");
      await expect(funds).toContainText("7.123456");
      await expectNoFundsForms(page);
      await expect(funds.locator(".crypto-table")).toHaveCount(0);
      if (role === "admin" && layout.width === 1440)
        await page.screenshot({
          path: testInfo.outputPath("admin-overview-desktop.png"),
          fullPage: true,
          animations: "disabled",
        });

      for (const business of businessSections) {
        if (layout.width === 1440)
          await page
            .locator(`a[href="${fixture.basePath}/${business.section}"]`)
            .first()
            .click();
        else await fixture.open(business.section);
        await expectSection(page, business.section);
        await expect(funds).toContainText(business.history);
        for (const other of businessSections)
          if (
            other.section !== business.section &&
            other.section !== "recipients"
          )
            await expect(funds).not.toContainText(other.history);
        await expectNoFundsForms(page);
        await funds
          .getByRole("button", { name: business.action, exact: true })
          .click();
        await expect(funds.locator("form[data-form]")).toHaveCount(1);
        await expect(
          funds.locator(`form[data-form="${business.form}"]`),
        ).toBeVisible();
        if (
          business.section === "settlements" &&
          ((role === "admin" && layout.width === 1440) ||
            (role === "merchant" && layout.width === 375))
        )
          await page.screenshot({
            path: testInfo.outputPath(
              `${role}-settlement-open-${layout.width}.png`,
            ),
            fullPage: true,
            animations: "disabled",
          });
        await funds
          .getByRole("button", { name: "取消申请", exact: true })
          .click();
        await expectNoFundsForms(page);
        await expectNoOverflow(page);
      }
      if (role === "admin") {
        await fixture.open("operations");
        await expectSection(page, "operations");
        await expectNoFundsForms(page);
        for (const operation of [
          { action: "登记受管地址", form: "address" },
          { action: "恢复归集", form: "sweep-retry" },
        ]) {
          await funds
            .getByRole("button", { name: operation.action, exact: true })
            .click();
          await expect(funds.locator("form[data-form]")).toHaveCount(1);
          await expect(
            funds.locator(`form[data-form="${operation.form}"]`),
          ).toBeVisible();
          await funds
            .getByRole("button", { name: "取消申请", exact: true })
            .click();
          await expectNoFundsForms(page);
        }
        await fixture.open("reconciliation");
        await expectSection(page, "reconciliation");
        await expectNoFundsForms(page);
        await expect(funds.locator(".crypto-table")).toHaveCount(0);
        await funds
          .getByRole("button", { name: "查询账本对账结果", exact: true })
          .click();
        await expect(funds).toContainText("账簿不平衡");
      } else {
        for (const section of ["operations", "reconciliation"])
          await expect(
            page.locator(`a[href="${fixture.basePath}/${section}"]`),
          ).toHaveCount(0);
      }
      await expectNoOverflow(page);
      expect(fixture.writes).toEqual([]);
      expect(fixture.errors).toEqual([]);
    });
  }
}

for (const role of ["admin", "merchant", "delegated"] as const) {
  for (const layout of layouts) {
    test(`${role} paused USDT keeps history readable at ${layout.width} ${layout.theme} without funds forms`, async ({
      page,
    }) => {
      await useLayout(page, layout);
      const fixture = await mockConsole(page, role, "paused");
      await fixture.open();
      const funds = page.locator(".crypto-funds");
      await expect(page.getByTestId("crypto-service-readonly")).toContainText(
        "USDT 收款与结算已暂停",
      );
      await expect(funds).toContainText("7.123456");
      await expect(funds).toContainText("0.000001");
      await expect(page.locator(".crypto-table")).toHaveCount(0);
      if (role === "delegated")
        await expect(funds).toContainText("当前为代登录会话，仅能查看记录");
      await expectNoFundsForms(page);
      for (const business of businessSections) {
        await fixture.open(business.section);
        await expectSection(page, business.section);
        await expect(funds).toContainText(business.history);
        await expect(
          funds.getByRole("button", { name: business.action, exact: true }),
        ).toHaveCount(0);
        await expectNoFundsForms(page);
        const writeButtons = funds.getByRole("button", {
          name: /核对并审批|申请核对取消|撤销绑定|使用 TRON/,
        });
        for (const button of await writeButtons.all())
          await expect(button).toBeDisabled();
      }
      for (const endpoint of [
        "balances",
        "invoices",
        "withdrawals",
        "recipients",
      ])
        expect(fixture.cryptoReads).toContain(
          `/console/v1/${fixture.area}/crypto/${endpoint}`,
        );
      await expectNoOverflow(page);
      expect(fixture.writes).toEqual([]);
      expect(fixture.errors).toEqual([]);
    });
  }
}

for (const mode of ["config-error", "list-error"] as const) {
  for (const role of ["admin", "merchant"] as const) {
    test(`${role} ${mode} clears stale USDT history and recovery keeps the form folded`, async ({
      page,
    }) => {
      await useLayout(page, role === "admin" ? layouts[0] : layouts[1]);
      const fixture = await mockConsole(page, role, "enabled");
      await fixture.open("invoices");
      const funds = page.locator(".crypto-funds");
      await expect(funds).toContainText("synthetic-usdt-order-history");
      await expectNoFundsForms(page);
      await funds
        .getByRole("button", { name: "创建收款单", exact: true })
        .click();
      await expect(
        page.locator('[data-form="invoice"] button[type="submit"]'),
      ).toBeEnabled();
      fixture.setMode(mode);
      await page.getByRole("button", { name: "刷新记录", exact: true }).click();
      await expect(page.getByTestId("crypto-service-error")).toContainText(
        "USDT 记录暂时无法读取",
      );
      await expect(funds).not.toContainText("synthetic-usdt-order-history");
      await expect(funds).not.toContainText("7.123456");
      await expectNoFundsForms(page);
      fixture.setMode("enabled");
      await page.getByRole("button", { name: "刷新记录", exact: true }).click();
      await expect(funds).toContainText("synthetic-usdt-order-history");
      await expect(page.getByTestId("crypto-service-error")).toHaveCount(0);
      await expectNoFundsForms(page);
      await expect(
        funds.getByRole("button", { name: "创建收款单", exact: true }),
      ).toBeEnabled();
      await expectNoOverflow(page);
      expect(fixture.writes).toEqual([]);
      expect(fixture.errors).toEqual([]);
    });
  }
}

for (const layout of layouts) {
  test(`unprivileged admin cannot open USDT at ${layout.width} ${layout.theme}`, async ({
    page,
  }) => {
    await useLayout(page, layout);
    const fixture = await mockConsole(page, "operator", "enabled");
    for (const section of [
      "overview",
      "invoices",
      "settlements",
      "refunds",
      "recipients",
      "operations",
      "reconciliation",
    ] as const) {
      await fixture.open(section);
      await expect(
        page.getByRole("heading", {
          name: "您没有访问此页面的权限",
          exact: true,
        }),
      ).toBeVisible();
      await expect(page.locator(".crypto-funds")).toHaveCount(0);
    }
    await expectNoOverflow(page);
    expect(fixture.cryptoReads).toEqual([]);
    expect(fixture.writes).toEqual([]);
    expect(fixture.errors).toEqual([]);
  });
}

test("delegated merchant with enabled USDT can read history without funds forms", async ({
  page,
}) => {
  await useLayout(page, layouts[0]);
  const fixture = await mockConsole(page, "delegated", "enabled");
  for (const business of businessSections) {
    await fixture.open(business.section);
    await expectSection(page, business.section);
    await expect(page.locator(".crypto-funds")).toContainText(
      "当前为代登录会话，仅能查看记录",
    );
    await expect(page.locator(".crypto-funds")).toContainText(business.history);
    await expectNoFundsForms(page);
    await expect(
      page.locator(".crypto-funds").getByRole("button", {
        name: business.action,
        exact: true,
      }),
    ).toHaveCount(0);
  }
  await expectNoOverflow(page);
  expect(fixture.writes).toEqual([]);
  expect(fixture.errors).toEqual([]);
});

for (const role of ["admin", "merchant"] as const) {
  test(`${role} keeps the same unknown USDT intent across business routes and form cancellation without a POST`, async ({
    page,
  }) => {
    await useLayout(page, layouts[1]);
    const fixture = await mockConsole(page, role, "enabled");
    const hash = (value: unknown) =>
      createHash("sha256").update(JSON.stringify(value)).digest("hex");
    const principal = hash([role, role === "admin" ? "1" : "1000", ""]),
      target = "/crypto/withdrawals",
      body = {
        merchant_id: "1000",
        request_id: "synthetic-original-unknown-usdt-request",
        network: "tron",
        address: historyAddress,
        amount: "2.5",
      },
      original = {
        version: 1,
        principal,
        target,
        body,
        digest: hash([principal, target, body]),
      },
      key = `goepay.crypto-intent.${principal}`;
    await page.addInitScript(
      ({ key, value }) => {
        const marker = "synthetic-usdt-intent-initialized";
        if (!sessionStorage.getItem(marker)) {
          localStorage.setItem(key, value);
          sessionStorage.setItem(marker, "1");
        }
      },
      { key, value: JSON.stringify(original) },
    );
    await fixture.open("settlements");
    const funds = page.locator(".crypto-funds"),
      pending = funds
        .getByRole("heading", { name: "原资金请求待核对", exact: true })
        .locator("..");
    await expectSection(page, "settlements");
    await expect(pending).toContainText(body.request_id);
    await expectNoFundsForms(page);
    await funds
      .getByRole("button", { name: "申请原链结算", exact: true })
      .click();
    const withdrawal = funds.locator('form[data-form="withdrawal"]');
    await expect(withdrawal.locator('input[name="request_id"]')).toHaveValue(
      body.request_id,
    );
    await expect(withdrawal.locator('input[name="amount"]')).toHaveValue("2.5");
    await expect(withdrawal.locator('input[name="address"]')).toHaveValue(
      historyAddress,
    );
    await expect(withdrawal.locator('button[type="submit"]')).toBeDisabled();
    await funds.getByRole("button", { name: "取消申请", exact: true }).click();
    await expectNoFundsForms(page);
    await expect(pending).toContainText(body.request_id);

    await page.locator(`a[href="${fixture.basePath}/refunds"]`).first().click();
    await expectSection(page, "refunds");
    await expect(pending).toContainText(body.request_id);
    await expectNoFundsForms(page);
    await page
      .locator(`a[href="${fixture.basePath}/settlements"]`)
      .first()
      .click();
    await expectSection(page, "settlements");
    await expect(pending).toContainText(body.request_id);
    await expectNoFundsForms(page);
    await funds
      .getByRole("button", { name: "申请原链结算", exact: true })
      .click();
    await expect(withdrawal.locator('input[name="request_id"]')).toHaveValue(
      body.request_id,
    );
    await expect(withdrawal.locator('input[name="amount"]')).toHaveValue("2.5");
    await expect(withdrawal.locator('button[type="submit"]')).toBeDisabled();
    await funds.getByRole("button", { name: "取消申请", exact: true }).click();
    await funds
      .getByRole("button", { name: "仅查询原请求", exact: true })
      .click();
    await expect(funds).toContainText("仍待核对");
    await expect
      .poll(() => page.evaluate((key) => localStorage.getItem(key), key))
      .toBe(JSON.stringify(original));
    expect(fixture.lookupReads.length).toBeGreaterThan(0);
    for (const url of fixture.lookupReads) {
      expect(new URL(url).searchParams.get("merchant_id")).toBe("1000");
      expect(new URL(url).searchParams.get("request_id")).toBe(body.request_id);
    }
    await expectNoFundsForms(page);
    await expectNoOverflow(page);
    expect(fixture.writes).toEqual([]);
    expect(fixture.errors).toEqual([]);
  });
}
