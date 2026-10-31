import { test, expect, type Page } from "@playwright/test";

const ok = (data: unknown, status = 200) => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
async function fixture(
  page: Page,
  mode:
    | "freeze-success"
    | "freeze-unknown"
    | "freeze-refused"
    | "freeze-preflight-unavailable"
    | "freeze-unknown-retry-refused"
    | "freeze-accepted-response-lost"
    | "freeze-success-refresh-failed"
    | "notify-queued"
    | "notify-cooldown",
) {
  let held = false,
    failRefresh = false,
    postCount = 0,
    detailReads = 0;
  const requests: string[] = [];
  const order = {
    id: "1",
    trade_no: "FEEDBACK212",
    out_trade_no: "local-feedback",
    merchant_id: "1000",
    type: "alipay",
    name: "Local feedback regression",
    amount: "100",
    status: "paid",
  };
  await page.route("**/console/v1/**", async (route) => {
    const r = route.request(),
      path = new URL(r.url()).pathname;
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: {
            id: "1",
            username: "admin",
            role: "admin",
            admin_class: "super",
          },
          csrf_token: "csrf-feedback212",
        }),
      );
    if (path.endsWith("/freeze-income") && r.method() === "POST") {
      requests.push(r.postDataJSON().external_no);
      postCount++;
      if (
        mode === "freeze-refused" ||
        (mode === "freeze-unknown-retry-refused" && postCount > 1)
      )
        return route.fulfill({
          status: 422,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "local explicit refusal" }),
        });
      if (
        (mode === "freeze-unknown" ||
          mode === "freeze-unknown-retry-refused") &&
        postCount === 1
      ) {
        failRefresh = true;
        return route.abort("connectionreset");
      }
      held = true;
      failRefresh = mode === "freeze-success-refresh-failed";
      if (mode === "freeze-accepted-response-lost")
        return route.abort("connectionreset");
      return route.fulfill(
        ok({
          id: "1",
          external_no: requests[0],
          state: "held",
          amount: "100",
          remaining_amount: "100",
          source_mode: "fresh",
        }),
      );
    }
    if (path.endsWith("/resend-notification") && r.method() === "POST") {
      requests.push(r.postDataJSON().external_no);
      postCount++;
      if (mode === "notify-cooldown")
        return route.fulfill({
          status: 429,
          headers: { "Retry-After": "60" },
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "local cooldown" }),
        });
      return route.fulfill(
        ok({ job_id: "22", state: "pending", attempts: 0 }, 202),
      );
    }
    if (path.endsWith("/orders/FEEDBACK212")) {
      detailReads++;
      if (failRefresh)
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({
            code: -1,
            message: "local detail unavailable",
          }),
        });
      return route.fulfill(
        ok({
          order,
          admin_control: { status: "", archived: false, revision: "0" },
          reporting: { refunded_amount: "0" },
          income_hold: held
            ? {
                id: "1",
                external_no: requests[0],
                state: "held",
                amount: "100",
                remaining_amount: "100",
                source_mode: "fresh",
              }
            : null,
          income_freeze_amount: "100",
          can_freeze_income: !held,
          can_unfreeze_income: held,
          can_resend_notification: true,
          notification:
            postCount && mode === "notify-queued"
              ? { job_id: "22", state: "pending", attempts: 0 }
              : null,
        }),
      );
    }
    if (path.endsWith("/orders"))
      return route.fulfill(
        ok({
          items: [{ ...order, income_status: held ? "held" : "none" }],
          total: 1,
        }),
      );
    if (
      mode === "freeze-preflight-unavailable" &&
      path.endsWith("/security/payment-password")
    )
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          message: "local security preflight unavailable",
        }),
      });
    if (path.endsWith("/security/payment-password"))
      return route.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    return route.fulfill(ok({ items: [], total: 0 }));
  });
  await page.goto("/admin/trade/order");
  await page
    .getByRole("button", { name: "订单操作 FEEDBACK212", exact: true })
    .click();
  await page
    .getByRole("menuitem", {
      name: mode.startsWith("freeze") ? "冻结订单" : "重新通知",
      exact: true,
    })
    .click();
  return {
    requests,
    reads: () => detailReads,
    restoreRefresh: () => {
      failRefresh = false;
    },
  };
}

test("freeze success feedback survives authoritative detail refresh", async ({
  page,
}) => {
  const { reads } = await fixture(page, "freeze-success");
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "解冻订单收入", exact: true }),
  ).toBeVisible();
  await expect(dialog.getByRole("status")).toContainText("平台订单收入已冻结");
});

test("uncertain freeze and failed refresh keep original error and retry intent", async ({
  page,
}) => {
  const { requests, reads } = await fixture(page, "freeze-unknown");
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByText("local detail unavailable", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("alert").filter({ hasText: "保留原冻结请求号" }),
  ).toContainText(requests[0]);
  await dialog
    .getByRole("button", { name: "重试原冻结请求", exact: true })
    .click();
  await expect(
    dialog
      .locator("dt")
      .filter({ hasText: /^冻结请求号$/ })
      .locator("xpath=following-sibling::dd[1]"),
  ).toHaveText(requests[0]);
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(2);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByRole("status")).toContainText("平台订单收入已冻结");
  expect(requests).toHaveLength(2);
  expect(requests[1]).toBe(requests[0]);
});

test("queued notification preserves original request and does not claim delivery", async ({
  page,
}) => {
  const { requests, reads } = await fixture(page, "notify-queued");
  const dialog = page.getByRole("dialog", {
    name: "重新通知确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByRole("status")).toContainText("尚未确认商户收到");
  await expect(dialog.getByLabel("本次通知幂等请求号")).toHaveValue(
    requests[0],
  );
  await expect(
    dialog.getByRole("button", { name: "发起新的通知重发", exact: true }),
  ).toHaveCount(0);
});

test("notification cooldown and original request survive detail refresh", async ({
  page,
}) => {
  const { requests, reads } = await fixture(page, "notify-cooldown");
  const dialog = page.getByRole("dialog", {
    name: "重新通知确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByRole("alert")).toContainText("冷却期");
  await expect(dialog.getByLabel("本次通知幂等请求号")).toHaveValue(
    requests[0],
  );
  await expect(dialog.getByText(/冷却剩余 \d+ 秒/)).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "重试原通知请求", exact: true }),
  ).toBeDisabled();
  expect(requests).toHaveLength(1);
});

test("lost freeze response resolves only the matching authoritative request", async ({
  page,
}) => {
  const { requests, reads } = await fixture(
    page,
    "freeze-accepted-response-lost",
  );
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByRole("status")).toContainText("已冻结");
  await expect(
    dialog.getByRole("alert").filter({ hasText: "冻结结果尚未核实" }),
  ).toHaveCount(0);
  expect(requests).toHaveLength(1);
});

test("confirmed freeze with failed refresh cannot start another intent from stale capabilities", async ({
  page,
}) => {
  const { requests, reads, restoreRefresh } = await fixture(
    page,
    "freeze-success-refresh-failed",
  );
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("local detail unavailable", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("status").filter({ hasText: "已冻结" }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "冻结订单收入", exact: true }),
  ).toBeDisabled();
  restoreRefresh();
  await dialog
    .getByRole("button", { name: "重新读取订单", exact: true })
    .click();
  await expect(
    dialog.getByRole("button", { name: "解冻订单收入", exact: true }),
  ).toBeEnabled();
  expect(requests).toHaveLength(1);
});

test("closing and reopening an uncertain freeze preserves its original request", async ({
  page,
}) => {
  const { requests, reads, restoreRefresh } = await fixture(
    page,
    "freeze-unknown",
  );
  let dialog = page.getByRole("dialog", { name: "冻结订单确认", exact: true });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  restoreRefresh();
  await page
    .getByRole("button", { name: "订单操作 FEEDBACK212", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "冻结订单", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "冻结订单确认", exact: true });
  await expect(
    dialog.getByRole("alert").filter({ hasText: "保留原冻结请求号" }),
  ).toContainText(requests[0]);
  await dialog
    .getByRole("button", { name: "重试原冻结请求", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(() => requests.length).toBe(2);
  expect(requests[1]).toBe(requests[0]);
});

test("closing and reopening notification preserves retry proof and cooldown", async ({
  page,
}) => {
  const { requests, reads } = await fixture(page, "notify-cooldown");
  let dialog = page.getByRole("dialog", { name: "重新通知确认", exact: true });
  await dialog
    .getByRole("button", { name: "确认通知请求", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page
    .getByRole("button", { name: "订单操作 FEEDBACK212", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "重新通知", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "重新通知确认", exact: true });
  await expect(dialog.getByLabel("本次通知幂等请求号")).toHaveValue(
    requests[0],
  );
  await expect(
    dialog.getByRole("button", { name: "重试原通知请求", exact: true }),
  ).toBeDisabled();
  await expect(dialog.getByText(/冷却剩余 \d+ 秒/)).toBeVisible();
  expect(requests).toHaveLength(1);
});

test("first explicit refusal is explained as rejected rather than an unknown payment", async ({
  page,
}) => {
  await fixture(page, "freeze-refused");
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("已明确拒绝");
  await expect(dialog).not.toContainText("冻结结果尚未核实");
});

test("refused retry cannot erase an earlier uncertain freeze", async ({
  page,
}) => {
  const { requests, reads } = await fixture(
    page,
    "freeze-unknown-retry-refused",
  );
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect.poll(reads).toBeGreaterThan(1);
  await expect(
    dialog.getByText("正在读取原订单…", { exact: true }),
  ).toHaveCount(0);
  await dialog
    .getByRole("button", { name: "重试原冻结请求", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect(
    dialog.getByRole("alert").filter({ hasText: "冻结结果尚未核实" }),
  ).toContainText(requests[0]);
  expect(requests).toHaveLength(2);
  expect(requests[1]).toBe(requests[0]);
});

test("security preflight failure is shown as not sent without creating an uncertain freeze", async ({
  page,
}) => {
  const { requests } = await fixture(page, "freeze-preflight-unavailable");
  const dialog = page.getByRole("dialog", {
    name: "冻结订单确认",
    exact: true,
  });
  await dialog
    .getByRole("button", { name: "确认冻结收入", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("尚未发送");
  await expect(dialog).not.toContainText("冻结结果尚未核实");
  expect(requests).toHaveLength(0);
});
