import { expect, test, type Page } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";

const id = "9007199254740993";
const endpoint = "/console/v1/merchant/account/event-subscription";
const callback = "https://merchant-refund201.invalid/events";
const initial = {
  event_url: callback,
  event_types: ["refund"],
  fingerprint: "a".repeat(64),
  consecutive_failures: 0,
  last_delivery_state: "none",
  subscription_notice: "",
};
const ok = (data: unknown, status = 200, message = "") => ({
  status,
  contentType: "application/json",
  body: JSON.stringify({ code: status === 200 ? 0 : status, data, message }),
});

async function fixture(
  page: Page,
  options: { delegated?: boolean; failure?: "conflict" | "network" } = {},
) {
  const reads: string[] = [],
    writes: { path: string; body: Record<string, unknown> }[] = [],
    directCallbacks: string[] = [],
    errors: string[] = [];
  let subscription = { ...initial, event_types: [...initial.event_types] };
  let notice = accountSettingsReadFixture("/account/notice", id) as Record<
    string,
    unknown
  >;
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("https://merchant-refund201.invalid/**", (route) => {
    directCallbacks.push(route.request().url());
    return route.abort("blockedbyclient");
  });
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (request.method() === "GET") reads.push(path);
    else writes.push({ path, body: request.postDataJSON() });
    if (path.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id, role: "merchant", name: "Synthetic refund201 merchant" },
          csrf_token: "refund201-browser-own-session",
          ...(options.delegated
            ? { delegated_by: { id: "1", name: "Synthetic admin201" } }
            : {}),
        }),
      );
    if (path === endpoint) {
      if (request.method() === "PATCH") {
        expect(request.headers()["x-csrf-token"]).toBe(
          "refund201-browser-own-session",
        );
        const body = request.postDataJSON();
        expect(Object.keys(body).sort()).toEqual([
          "event_types",
          "event_url",
          "expected_fingerprint",
        ]);
        expect(body.expected_fingerprint).toBe(subscription.fingerprint);
        if (options.failure === "conflict") {
          subscription = {
            ...subscription,
            event_url: "https://merchant-refund201.invalid/concurrent",
            fingerprint: "c".repeat(64),
          };
          return route.fulfill(ok(null, 409, "事件订阅已被其他页面修改"));
        }
        subscription = {
          ...subscription,
          event_url: body.event_url,
          event_types: body.event_types,
          fingerprint:
            subscription.fingerprint === "a".repeat(64)
              ? "b".repeat(64)
              : "c".repeat(64),
        };
        if (options.failure === "network") return route.abort("failed");
      }
      return route.fulfill(ok(subscription));
    }
    if (path.endsWith("/account/api-info"))
      return route.fulfill(
        ok({
          pid: id,
          algorithm: "MD5",
          api_key_masked: "synthetic****201",
          merchant_public_key: "",
          platform_public_key: "",
          can_rotate_rsa: false,
        }),
      );
    if (path.endsWith("/account/signature-policy"))
      return route.fulfill(
        ok({
          merchant_id: id,
          keytype: 0,
          algorithm: "MD5",
          rsa_configured: false,
          fingerprint: "d".repeat(64),
          can_update: true,
        }),
      );
    if (path.endsWith("/account/notice")) {
      if (request.method() === "PUT") {
        const body = request.postDataJSON();
        expect(Object.keys(body).sort()).toEqual([
          "clear_fields",
          "expected_profile_id",
          "msgconfig",
          "targets",
          "voice_order",
        ]);
        notice = {
          ...notice,
          id: "1",
          revision: "1",
          msgconfig: body.msgconfig,
          voice_order: body.voice_order,
        };
      }
      return route.fulfill(ok(notice));
    }
    if (path === "/console/v1/public/merchant-access")
      return route.fulfill(
        ok({
          close_keylogin: 0,
          user_refund: 1,
        }),
      );
    if (path.endsWith("/account/profile"))
      return route.fulfill(
        ok({
          uid: id,
          name: "Synthetic refund201 merchant",
          email: "",
          has_pwd: true,
          pay: "1",
        }),
      );
    return route.fulfill(
      ok(
        accountSettingsReadFixture(path, id) ?? {
          items: [],
          total: 0,
          enabled: false,
        },
      ),
    );
  });
  await page.goto("/merchant/account/api-info");
  const panel = page.getByRole("region", {
    name: "事件消息订阅设置",
    exact: true,
  });
  return { panel, reads, writes, directCallbacks, errors };
}

for (const width of [1440, 375]) {
  test(`refund201 original subscription is explicit, keyboard accessible and contained at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await expect(f.panel.getByLabel("订阅URL", { exact: true })).toHaveValue(
      callback,
    );
    await expect(
      f.panel.getByLabel("订单退款", { exact: false }),
    ).toBeChecked();
    await expect(f.panel.getByLabel("交易投诉", { exact: true })).toHaveCount(
      0,
    );
    await expect(f.panel).toContainText(
      "仅发送在平台提交的退款操作事件，不包括通过API退款的操作",
    );
    const regions = await page
      .locator("section[aria-label]")
      .evaluateAll((sections) =>
        sections.map((section) => section.getAttribute("aria-label")),
      );
    expect(regions.indexOf("签名方式设置")).toBeGreaterThanOrEqual(0);
    expect(regions.indexOf("事件消息订阅设置")).toBeGreaterThan(
      regions.indexOf("签名方式设置"),
    );
    const url = f.panel.getByLabel("订阅URL", { exact: true });
    await url.fill("https://merchant-refund201.invalid/keyboard-draft");
    await url.press("Tab");
    await expect(
      f.panel.getByLabel("订单退款", { exact: false }),
    ).toBeFocused();
    const save = f.panel.getByRole("button", { name: "保存订阅", exact: true });
    await save.press("Enter");
    const confirm = f.panel.getByRole("button", {
      name: "确认保存",
      exact: true,
    });
    await expect(confirm).toBeFocused();
    expect(f.writes).toHaveLength(0);
    await confirm.press("Escape");
    await expect(save).toBeFocused();
    await expect(url).toHaveValue(
      "https://merchant-refund201.invalid/keyboard-draft",
    );
    await save.press("Enter");
    await confirm.press("Enter");
    await expect(f.panel.getByRole("status")).toContainText("订阅设置已保存");
    expect(f.writes).toEqual([
      {
        path: endpoint,
        body: {
          event_url: "https://merchant-refund201.invalid/keyboard-draft",
          event_types: ["refund"],
          expected_fingerprint: "a".repeat(64),
        },
      },
    ]);
    await expect(f.panel).not.toContainText(/回调已送达|消息已送达|已发送测试/);
    await f.panel
      .getByRole("button", { name: "禁用订阅", exact: true })
      .click();
    expect(f.writes).toHaveLength(1);
    await f.panel
      .getByRole("button", { name: "确认禁用", exact: true })
      .press("Enter");
    await expect(
      f.panel.getByLabel("订单退款", { exact: false }),
    ).not.toBeChecked();
    await expect(url).toHaveValue(
      "https://merchant-refund201.invalid/keyboard-draft",
    );
    expect(f.writes[1]).toEqual({
      path: endpoint,
      body: {
        event_url: "https://merchant-refund201.invalid/keyboard-draft",
        event_types: [],
        expected_fingerprint: "b".repeat(64),
      },
    });
    await f.panel.scrollIntoViewIfNeeded();
    const bounds = await f.panel.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(
      await page.evaluate(() => localStorage.length + sessionStorage.length),
    ).toBe(0);
    expect(f.directCallbacks).toEqual([]);
    expect(f.errors).toEqual([]);
    await f.panel.screenshot({
      path: info.outputPath(`refund201-subscription-${width}.png`),
    });
  });
}

for (const failure of ["conflict", "network"] as const) {
  test(`refund201 ${failure} locks writes until a read-only subscription recheck`, async ({
    page,
  }) => {
    const f = await fixture(page, { failure });
    const url = f.panel.getByLabel("订阅URL", { exact: true });
    await expect(url).toHaveValue(callback);
    await url.fill("https://merchant-refund201.invalid/uncertain");
    await f.panel
      .getByRole("button", { name: "保存订阅", exact: true })
      .click();
    await f.panel
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(f.panel.getByRole("alert")).toContainText("重新读取");
    await expect(f.panel.locator("form")).toHaveCount(0);
    expect(f.writes.filter((write) => write.path === endpoint)).toHaveLength(1);
    expect(f.reads.filter((path) => path === endpoint)).toHaveLength(1);
    await f.panel
      .getByRole("button", { name: "重新读取订阅", exact: true })
      .click();
    await expect(f.panel.getByLabel("订阅URL", { exact: true })).toHaveValue(
      failure === "conflict"
        ? "https://merchant-refund201.invalid/concurrent"
        : "https://merchant-refund201.invalid/uncertain",
    );
    expect(f.writes.filter((write) => write.path === endpoint)).toHaveLength(1);
    expect(f.reads.filter((path) => path === endpoint)).toHaveLength(2);
    expect(f.directCallbacks).toEqual([]);
  });
}

test("refund201 subscription and existing message push keep their independent payloads and saved values", async ({
  page,
}) => {
  const f = await fixture(page);
  await expect(f.panel.getByLabel("订阅URL", { exact: true })).toHaveValue(
    callback,
  );
  await f.panel
    .getByLabel("订阅URL", { exact: true })
    .fill("https://merchant-refund201.invalid/independent");
  await f.panel.getByRole("button", { name: "保存订阅", exact: true }).click();
  await f.panel.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(f.panel.getByRole("status")).toContainText("订阅设置已保存");
  expect(f.writes.map((write) => write.path)).toEqual([endpoint]);
  await page.goto("/merchant/account/settings");
  const notice = page.locator(".notice-settings");
  await expect(
    notice.getByRole("heading", { name: "消息推送", exact: true }),
  ).toBeVisible();
  await expect(notice.getByRole("combobox", { name: "订单支付成功", exact: true })).toHaveValue(
    "0",
  );
  await notice.getByRole("combobox", { name: "订单支付成功", exact: true }).selectOption("2");
  await notice.getByRole("button", { name: "保存设置", exact: true }).click();
  await expect(notice.getByRole("status")).toHaveText("通知设置已保存");
  expect(f.writes.map((write) => write.path)).toEqual([
    endpoint,
    "/console/v1/merchant/account/notice",
  ]);
  expect(f.writes[1].body).not.toHaveProperty("event_url");
  expect(f.writes[1].body).not.toHaveProperty("event_types");
  expect(f.writes[0].body).not.toHaveProperty("msgconfig");
  await page.goto("/merchant/account/api-info");
  await expect(f.panel.getByLabel("订阅URL", { exact: true })).toHaveValue(
    "https://merchant-refund201.invalid/independent",
  );
  await expect(f.panel.getByLabel("订单退款", { exact: false })).toBeChecked();
  expect(f.writes).toHaveLength(2);
  expect(f.directCallbacks).toEqual([]);
});

test("refund201 delegated API information has no editable subscription and never reads its URL", async ({
  page,
}) => {
  const f = await fixture(page, { delegated: true });
  await expect(f.panel).toContainText("本人");
  await expect(f.panel.locator("form")).toHaveCount(0);
  expect(f.reads.filter((path) => path === endpoint)).toEqual([]);
  expect(f.writes).toEqual([]);
});
