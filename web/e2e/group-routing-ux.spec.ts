import { test, expect } from "@playwright/test";
import type { Page, Route } from "@playwright/test";
import { inheritedRiskGroup } from "./fixtures/core-risk187";

test("a new original group starts with both SDK payment types closed until explicitly configured", async ({
  page,
}) => {
  const { dialog, writes } = await mockRoutingEditor(page, (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data: { available: true, items: [] } }),
    }),
  );
  await dialog
    .getByRole("button", { name: "关闭用户组编辑", exact: true })
    .click();
  await page.getByRole("button", { name: "新增用户组", exact: true }).click();
  const create = page.getByRole("dialog", { name: "新增用户组", exact: true });
  await expect(
    create.getByRole("combobox", { name: "支付宝通道", exact: true }),
  ).toHaveValue("disabled");
  await expect(
    create.getByRole("combobox", { name: "微信支付通道", exact: true }),
  ).toHaveValue("disabled");
  await create.getByLabel("显示名称", { exact: true }).fill("原版新组");
  expect(writes).toHaveLength(0);
  await create.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]!.info).toEqual({
    alipay: { channel: "0", type: "", rate: "" },
    wxpay: { channel: "0", type: "", rate: "" },
  });
});

for (const width of [1280, 375])
  test(`original group route and retained rate stay bound to actual long IDs at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const gid = "9007199254740993",
      channel = "9007199254740995",
      roll = "9223372036854775807",
      writes: Record<string, unknown>[] = [];
    const record = {
      id: gid,
      name: "原用户组",
      rate_bps: 38,
      info: {
        alipay: { rate: "99.50", channel, type: "channel" },
        wxpay: { rate: "98.75" },
      },
    };
    await page.route("**/console/v1/**", (route) => {
      const request = route.request(),
        url = new URL(request.url());
      let data: unknown = { items: [], total: 0 };
      if (request.method() !== "GET") {
        writes.push(request.postDataJSON());
        data = { ...record, ...request.postDataJSON() };
      } else if (url.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super", name: "local" },
          csrf_token: "group152",
        };
      else if (url.pathname.endsWith("/payment-risk/groups/" + gid))
        data = inheritedRiskGroup(gid, "原用户组");
      else if (url.pathname.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            {
              name: "rate_bps",
              label: "费率（万分比）",
              type: "integer",
              min: 0,
              max: 10000,
            },
            { name: "info", label: "支付方式分成比例", type: "json" },
          ],
          permissions: { create: true, update: true, delete: true },
        };
      else if (url.pathname.endsWith("/groups/routing-options"))
        data = {
          available: true,
          items: [
            { id: channel, type: "alipay", methods: ["native"] },
            { id: "8", type: "wxpay", methods: ["native"] },
          ],
        };
      else if (url.pathname.endsWith("/pay-rolls"))
        data = {
          items: [
            {
              id: roll,
              name: "原轮询名称",
              merchant_id: "0",
              type: "alipay",
              enabled: true,
              strategy: "first",
              channel_ids: channel + ":1",
            },
          ],
          total: 1,
        };
      else if (url.pathname.endsWith("/groups/" + gid)) data = record;
      else if (url.pathname.endsWith("/groups"))
        data = { items: [record], total: 1 };
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/merchant/group-list");
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "编辑用户组 GID:" + gid,
      exact: true,
    });
    const select = dialog.getByRole("combobox", {
      name: "支付宝通道",
      exact: true,
    });
    await expect(select).toBeEnabled();
    await expect(select).toHaveValue("channel:" + channel);
    await expect(
      dialog.getByLabel("支付宝分成比例（%）", { exact: true }),
    ).toHaveValue("99.50");
    await select.selectOption("roll:" + roll);
    await dialog
      .getByLabel("支付宝分成比例（%）", { exact: true })
      .fill("97.25");
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0]!.info).toEqual({
      alipay: { rate: "97.25", channel: roll, type: "roll" },
      wxpay: { rate: "98.75" },
    });
    await expect(dialog).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });

async function mockRoutingEditor(
  page: Page,
  catalog: (route: Route) => Promise<void> | void,
  originalChannel = "7",
) {
  const gid = "9007199254740993",
    writes: Record<string, unknown>[] = [],
    record = {
      id: gid,
      name: "保留原配置",
      rate_bps: 38,
      info: {
        alipay: { channel: originalChannel, type: "channel", rate: "99.50" },
      },
    };
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    let data: unknown = { items: [], total: 0 };
    if (request.method() !== "GET") {
      writes.push(request.postDataJSON());
      data = { ...record, ...request.postDataJSON() };
    } else if (path.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "super", name: "local" },
        csrf_token: "group152",
      };
    else if (path.endsWith("/payment-risk/groups/" + gid))
      data = inheritedRiskGroup(gid, "保留原配置");
    else if (path.endsWith("/groups/schema"))
      data = {
        title: "用户组设置",
        fields: [
          { name: "name", label: "名称", type: "string", required: true },
          { name: "rate_bps", label: "费率（万分比）", type: "integer" },
          { name: "info", label: "支付方式分成比例", type: "json" },
        ],
        permissions: { create: true, update: true, delete: true },
      };
    else if (path.endsWith("/groups/routing-options")) return catalog(route);
    else if (path.endsWith("/groups/" + gid)) data = record;
    else if (path.endsWith("/groups")) data = { items: [record], total: 1 };
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/merchant/group-list");
  const opener = page.getByRole("button", { name: "编辑", exact: true });
  await opener.click();
  return {
    writes,
    record,
    opener,
    dialog: page.getByRole("dialog", {
      name: "编辑用户组 GID:" + gid,
      exact: true,
    }),
  };
}

for (const width of [1280, 375])
  test(`original closed random sequential first options remain distinct at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const { dialog, writes } = await mockRoutingEditor(
      page,
      (route) =>
        route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            code: 0,
            data: { available: true, items: [] },
          }),
        }),
      "0",
    );
    const select = dialog.getByRole("combobox", {
      name: "支付宝通道",
      exact: true,
    });
    await expect(select).toBeEnabled();
    await expect(select).toHaveValue("disabled");
    for (const [value, label] of [
      ["disabled", "关闭"],
      ["random", "随机可用"],
      ["sequential", "顺序可用"],
      ["first", "首个可用通道"],
      ["default", "继承系统路由"],
    ]) {
      await expect(
        select.getByRole("option", { name: label, exact: true }),
      ).toHaveAttribute("value", value!);
    }
    await select.selectOption("random");
    await select.selectOption("sequential");
    await dialog.getByLabel("支付宝分成比例（%）", { exact: true }).fill("0");
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0]!.info).toEqual({
      alipay: { channel: "-4", type: "channel", rate: "0" },
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });

test("group routing catalog failure and explicit retry preserve the original route and edited rate", async ({
  page,
}) => {
  let fail = true;
  const { dialog, writes } = await mockRoutingEditor(page, (route) =>
    route.fulfill({
      status: fail ? 503 : 200,
      contentType: "application/json",
      body: JSON.stringify(
        fail
          ? { code: -1, message: "fixture unavailable" }
          : {
              code: 0,
              data: {
                available: true,
                items: [{ id: "7", type: "alipay", methods: ["native"] }],
              },
            },
      ),
    }),
  );
  const select = dialog.getByRole("combobox", {
    name: "支付宝通道",
    exact: true,
  });
  await expect(dialog.getByRole("alert")).toContainText("原配置已保留");
  await expect(select).toBeDisabled();
  await expect(select).toHaveValue("channel:7");
  await dialog.getByLabel("支付宝分成比例（%）", { exact: true }).fill("97.50");
  expect(writes).toHaveLength(0);
  fail = false;
  await dialog
    .getByRole("button", { name: "重试读取通道", exact: true })
    .click();
  await expect(select).toBeEnabled();
  await expect(select).toHaveValue("channel:7");
  await expect(
    dialog.getByLabel("支付宝分成比例（%）", { exact: true }),
  ).toHaveValue("97.50");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]!.info).toEqual({
    alipay: { channel: "7", type: "channel", rate: "97.50" },
  });
});

test("closing a pending group catalog cannot replace a subsequently opened editor options", async ({
  page,
}) => {
  let held: Route | undefined,
    calls = 0;
  const { dialog, opener, writes } = await mockRoutingEditor(page, (route) => {
    calls++;
    if (calls === 1) {
      held = route;
      return;
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        code: 0,
        data: {
          available: true,
          items: [{ id: "8", type: "alipay", methods: ["native"] }],
        },
      }),
    });
  });
  await expect.poll(() => calls).toBe(1);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  await opener.click();
  const select = dialog.getByRole("combobox", {
    name: "支付宝通道",
    exact: true,
  });
  await expect(select).toBeEnabled();
  expect(held).toBeDefined();
  try {
    await held!.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        code: 0,
        data: {
          available: true,
          items: [{ id: "9", type: "alipay", methods: ["native"] }],
        },
      }),
    });
  } catch {
    expect(held!.request().failure()?.errorText).toMatch(
      /ABORTED|ERR_FAILED|cancel/i,
    );
  }
  await expect(select.locator('option[value="channel:8"]')).toHaveCount(1);
  await expect(select.locator('option[value="channel:9"]')).toHaveCount(0);
  await expect(select).toHaveValue("channel:7");
  expect(writes).toHaveLength(0);
});
