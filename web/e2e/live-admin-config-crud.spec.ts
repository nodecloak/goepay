import { test, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { localCrudFixture } from "./local-crud-fixture";

test.describe("real local admin configuration operations", () => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1" ||
      process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Owned loopback simulation only",
  );
  let local: Awaited<ReturnType<typeof localCrudFixture>>;
  test.beforeAll(async ({ playwright }) => {
    local = await localCrudFixture(playwright, import.meta.url);
  });
  test.beforeEach(async ({ page }) => {
    await local.attach(page, "admin");
  });
  test.afterAll(async () => {
    await local?.dispose();
  });

  test("restricted operator can be promoted, persisted, downgraded and deleted through its own row", async ({
    page,
  }) => {
    await page.goto("/admin/system/admin-user");
    await page.getByRole("button", { name: "添加管理员", exact: true }).click();
    const dialog = page.getByRole("dialog");
    const username = local.tag + "-operator";
    await dialog.getByRole("textbox", { name: /账号$/ }).fill(username);
    await dialog
      .getByLabel("名称", { exact: false })
      .fill("Local restricted operator");
    await dialog
      .getByLabel("密码", { exact: false })
      .fill("synthetic-" + randomUUID());
    await dialog
      .getByRole("checkbox", { name: "订单管理", exact: true })
      .check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("cell", { name: username, exact: true }) });
    await expect(row).toBeVisible();
    const listed = await local.call(
      "admin",
      "/resources/admin-users?limit=100",
    );
    const saved = listed.items.find((r: any) => r.username === username);
    expect(saved.permissions).toEqual(["OrderList"]);
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog
      .getByRole("combobox", { name: /权限等级/ })
      .selectOption("super");
    const response = page.waitForResponse(
      (r) =>
        r.request().method() === "PATCH" &&
        new URL(r.url()).pathname.endsWith("/admin-users/" + saved.id),
    );
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    const updated = await response;
    expect(updated.ok(), JSON.stringify(await updated.json())).toBeTruthy();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(
      dialog.getByRole("combobox", { name: /权限等级/ }),
    ).toHaveValue("super");
    await dialog
      .getByRole("combobox", { name: /权限等级/ })
      .selectOption("operator");
    await dialog
      .getByRole("checkbox", { name: "订单管理", exact: true })
      .check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(
      (await local.call("admin", "/resources/admin-users/" + saved.id))
        .permissions,
    ).toEqual(["OrderList"]);
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认删除", exact: true })
      .click();
    await expect(row).toHaveCount(0);
    await page.reload();
    await expect(row).toHaveCount(0);
  });

  test("invitations generate, survive reload, cancel deletion and return from a depleted last page", async ({
    page,
  }) => {
    await page.goto("/admin/merchant/invite-code");
    await page.getByRole("button", { name: "生成邀请码", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByLabel("生成个数", { exact: true })
      .fill("21");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "生成", exact: true })
      .click();
    await expect(
      page.getByRole("dialog", { name: "生成结果", exact: true }),
    ).toContainText("成功生成 21 个邀请码");
    await page.getByRole("button", { name: "关闭结果", exact: true }).click();
    await page.reload();
    await expect(page.locator("tbody tr")).toHaveCount(20);
    await page.getByRole("button", { name: "下一页", exact: true }).click();
    await expect(page.locator("tbody tr")).toHaveCount(1);
    const row = page.locator("tbody tr");
    const code = await row.locator("td").first().innerText();
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    await expect(row).toContainText(code);
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认删除", exact: true })
      .click();
    await expect(page.locator("tbody tr")).toHaveCount(20);
    await expect(
      page.getByRole("button", { name: "上一页", exact: true }),
    ).toBeDisabled();
    expect(
      (await local.call("admin", "/invitation-codes?limit=100")).total,
    ).toBe(20);
    await page.getByRole("button", { name: "清空全部", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认清空", exact: true })
      .click();
    await expect(page.getByText("暂无邀请码", { exact: true })).toBeVisible();
    expect(
      (await local.call("admin", "/invitation-codes?limit=100")).total,
    ).toBe(0);
  });

  test("payment method create, validation, edit, toggle, cancellation and delete persist", async ({
    page,
  }) => {
    await page.goto("/admin/payapi/pay-type");
    let writes = 0;
    page.on("request", (r) => {
      if (
        new URL(r.url()).pathname.includes("/resources/pay-types") &&
        r.method() !== "GET"
      )
        writes++;
    });
    await page.getByRole("button", { name: "新增", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(
      dialog.getByLabel("显示名称", { exact: false }),
    ).toHaveAttribute("aria-invalid", "true");
    expect(writes).toBe(0);
    const name = local.tag + "-paytype";
    await dialog.getByLabel("调用值", { exact: false }).selectOption("ecny");
    await dialog.getByLabel("显示名称", { exact: false }).fill(name);
    await dialog.getByLabel("支持设备", { exact: false }).selectOption("1");
    await dialog.getByLabel("状态", { exact: true }).uncheck();
    const creating = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/console/v1/admin/resources/pay-types",
    );
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    const created = await (await creating).json();
    expect(created.code).toBe(0);
    const id = created.data.id;
    await expect(dialog).toHaveCount(0);
    let row = page.getByRole("row").filter({ hasText: name });
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(dialog.getByLabel("支持设备", { exact: false })).toHaveValue(
      "1",
    );
    await dialog
      .getByLabel("显示名称", { exact: false })
      .fill(name + "-edited");
    await dialog.getByLabel("支持设备", { exact: false }).selectOption("2");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await page.reload();
    row = page.getByRole("row").filter({ hasText: name + "-edited" });
    await expect(row).toContainText("Mobile");
    await row.getByRole("switch").click();
    await expect
      .poll(
        async () =>
          (await local.call("admin", "/resources/pay-types/" + id)).enabled,
      )
      .toBe(true);
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    expect((await local.call("admin", "/resources/pay-types/" + id)).name).toBe(
      name + "-edited",
    );
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await expect(row).toHaveCount(0);
    await page.reload();
    await expect(row).toHaveCount(0);
    expect(
      (await local.request("admin", "/resources/pay-types/" + id)).status(),
    ).toBe(404);
  });

  for (const kind of ["pay-types", "groups"] as const)
    test(`a delivered ${kind} creation with a lost response cannot be resubmitted`, async ({
      page,
    }) => {
      const pay = kind === "pay-types",
        name = local.tag + "-lost-" + kind;
      await page.goto(
        pay ? "/admin/payapi/pay-type" : "/admin/merchant/group-list",
      );
      let attempts = 0,
        completed = 0;
      await page.route(
        "**/console/v1/admin/resources/" + kind,
        async (route) => {
          if (route.request().method() !== "POST") return route.continue();
          attempts++;
          const response = await route.fetch();
          const body = await response.json();
          if (attempts === 1) {
            expect(response.ok(), body.message).toBeTruthy();
            expect(body.code).toBe(0);
          }
          completed++;
          await route.abort("failed");
        },
      );
      await page
        .getByRole("button", { name: pay ? "新增" : "新增用户组", exact: true })
        .click();
      const dialog = page.getByRole("dialog"),
        save = dialog.getByRole("button", { name: "保存", exact: true });
      await dialog.getByLabel("显示名称", { exact: false }).fill(name);
      if (pay)
        await dialog
          .getByLabel("调用值", { exact: false })
          .selectOption("ecny");
      await save.click();
      await expect.poll(() => completed).toBe(1);
      await expect(dialog.getByRole("alert")).toBeVisible();
      if (await save.isEnabled()) {
        await save.click();
        await expect.poll(() => attempts).toBe(2);
      }
      expect(attempts).toBe(1);
      await expect(save).toBeDisabled();
      await dialog
        .getByRole("button", { name: "关闭并核对列表", exact: true })
        .click();
      await expect(dialog).toHaveCount(0);
      const list = await local.call(
        "admin",
        "/resources/" + kind + "?limit=100",
      );
      expect(list.items.filter((r: any) => r.name === name)).toHaveLength(1);
      await expect(
        page.getByRole("row").filter({ hasText: name }),
      ).toBeVisible();
    });
});
