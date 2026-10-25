import { test, expect } from "@playwright/test";
import { transferListFixture as fixture } from "../tests/fixtures/transfer-list";
for (const width of [1280, 375]) {
  test(`a mismatched transfer detail cannot trigger row query at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const row = fixture({ status: "unknown" });
    let wrong: Record<string, unknown> = {},
      queries = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      let data: unknown = { items: [row], total: 1 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "transfer150",
        };
      else if (r.method() !== "GET") {
        queries++;
        data = row;
      } else if (p.endsWith("/transfers/" + row.id))
        data = {
          transfer: { ...row, ...wrong },
          recipient: { account: "***0150", name: "***" },
        };
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/transfer/list");
    const button = page
      .locator('tr[data-transfer-id="' + row.id + '"]')
      .getByRole("button", { name: "查询状态", exact: true });
    for (const patch of [
      { id: "9007199254740994" },
      { merchant_id: "1" },
      { amount: "103" },
    ]) {
      wrong = patch;
      await button.click();
      await expect(page.getByRole("alert")).toContainText(
        "付款详情与原记录不一致",
      );
      await expect(button).toBeEnabled();
      await expect(
        page.getByRole("dialog", { name: "付款详情", exact: true }),
      ).toHaveCount(0);
    }
    expect(queries).toBe(0);
  });
  test(`original transfer row query checks only the original payment at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const row = fixture({ status: "unknown" }),
      unsent = fixture({
        id: "9007199254740994",
        status: "unsent",
        settlement_send_state: "unsent",
      });
    let queries = 0,
      otherWrites = 0;
    await page.route("**/console/v1/**", (route) => {
      const r = route.request(),
        p = new URL(r.url()).pathname;
      let data: unknown = { items: [row, unsent], total: 2 },
        status = 200;
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "transfer150",
        };
      else if (
        r.method() === "POST" &&
        p.endsWith("/transfers/" + row.id + "/query")
      ) {
        queries++;
        data = row;
        status = 202;
      } else if (r.method() !== "GET") otherWrites++;
      else if (p.endsWith("/transfers/" + row.id))
        data = {
          transfer: row,
          recipient: { account: "***0150", name: "***" },
        };
      return route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/transfer/list");
    const button = page
      .locator('tr[data-transfer-id="' + row.id + '"]')
      .getByRole("button", { name: "查询状态", exact: true });
    await expect(
      page
        .locator('tr[data-transfer-id="' + unsent.id + '"]')
        .getByRole("button", { name: "查询状态", exact: true }),
    ).toHaveCount(0);
    const response = page.waitForResponse((r) =>
      new URL(r.url()).pathname.endsWith("/transfers/" + row.id + "/query"),
    );
    await button.click();
    expect((await response).status()).toBe(202);
    const dialog = page.getByRole("dialog", { name: "付款详情", exact: true });
    await expect(dialog.getByRole("status")).toContainText("尚未核实");
    await dialog.getByRole("button", { name: "关闭详情", exact: true }).click();
    await expect(button).toBeFocused();
    await page
      .locator('tr[data-transfer-id="' + row.id + '"]')
      .getByRole("button", { name: "付款详情", exact: true })
      .click();
    await expect(dialog).toBeVisible();
    expect(queries).toBe(1);
    expect(otherWrites).toBe(0);
  });
  test(`closing a route before transfer detail resolves cannot query its provider at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const row = fixture({ status: "unknown" });
    let queries = 0,
      release!: () => void,
      started!: () => void,
      finished!: () => void;
    const gate = new Promise<void>((r) => (release = r)),
      readStarted = new Promise<void>((r) => (started = r)),
      readFinished = new Promise<void>((r) => (finished = r));
    await page.route("**/console/v1/**", async (route) => {
      const r = route.request(),
        u = new URL(r.url()),
        p = u.pathname;
      let data: unknown = {
        items: u.searchParams.get("merchant_id") === "2" ? [] : [row],
        total: u.searchParams.get("merchant_id") === "2" ? 0 : 1,
      };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "transfer150",
        };
      else if (r.method() === "POST") {
        queries++;
        data = row;
      } else if (p.endsWith("/transfers/" + row.id)) {
        started();
        await gate;
        try {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
              code: 0,
              data: {
                transfer: row,
                recipient: { account: "***0150", name: "***" },
              },
            }),
          });
        } finally {
          finished();
        }
        return;
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    try {
      await page.goto("/admin/transfer/list");
      await page
        .locator('tr[data-transfer-id="' + row.id + '"]')
        .getByRole("button", { name: "查询状态", exact: true })
        .click();
      await readStarted;
      await page.evaluate(() => {
        history.pushState({}, "", "/admin/transfer/list?uid=2");
        window.dispatchEvent(new PopStateEvent("popstate"));
      });
      await expect(page.getByText("商户号：2", { exact: true })).toBeVisible();
      release();
      await readFinished;
      await expect(
        page.getByRole("dialog", { name: "付款详情", exact: true }),
      ).toHaveCount(0);
      expect(queries).toBe(0);
    } finally {
      release();
    }
  });
}
