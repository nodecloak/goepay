import { test, expect } from "@playwright/test";
for (const width of [1280, 375]) {
  test(`closing a pending group report cannot leak its late response into another group at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let release!: () => void, arrived!: () => void, finished!: () => void;
    const gate = new Promise<void>((resolve) => (release = resolve)),
      started = new Promise<void>((resolve) => (arrived = resolve)),
      done = new Promise<void>((resolve) => (finished = resolve)),
      writes: string[] = [];
    const report = (gid: string) => {
      const items = ["alipay", "wxpay", "sandbox"].map((type, index) => ({
          type,
          showname: ["支付宝", "微信支付", "本地模拟"][index],
          money: "0.00",
          count: "0",
          success_count: "0",
          rate: "0.00",
        })),
        summary = {
          type: "total",
          showname: "总计",
          money: "0.00",
          count: "0",
          success_count: "0",
          rate: "0.00",
        };
      return {
        group_id: gid,
        group_name: gid === "1" ? "迟到旧组" : "当前新组",
        timezone: "Asia/Shanghai",
        cohort: "current_effective_group",
        days: {
          today: { date: "2026-10-26", items, summary },
          yesterday: { date: "2026-10-25", items, summary },
        },
      };
    };
    await page.route("**/console/v1/**", async (route) => {
      const r = route.request(),
        u = new URL(r.url());
      if (r.method() !== "GET") writes.push(u.pathname);
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "group143",
        };
      else if (u.pathname.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: true, update: true, delete: true },
        };
      else if (u.pathname === "/console/v1/admin/resources/groups")
        data = {
          items: [
            { id: "1", name: "旧组" },
            { id: "2", name: "新组" },
          ],
          total: 2,
        };
      else if (u.pathname.endsWith("/reports/groups")) {
        const gid = u.searchParams.get("gid")!;
        if (gid === "1") {
          arrived();
          await gate;
          try {
            await route.fulfill({ json: { code: 0, data: report(gid) } });
          } catch {
          } finally {
            finished();
          }
          return;
        }
        data = report(gid);
      }
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/group-list");
    const rows = page.getByRole("row");
    await rows
      .filter({ has: page.getByRole("cell", { name: "旧组", exact: true }) })
      .getByRole("button", { name: "统计", exact: true })
      .click();
    await started;
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "关闭统计" })
      .click();
    await rows
      .filter({ has: page.getByRole("cell", { name: "新组", exact: true }) })
      .getByRole("button", { name: "统计", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading")).toHaveText(
      "用户组 当前新组 支付统计",
    );
    release();
    await done;
    await expect(dialog).not.toContainText("迟到旧组");
    await expect(dialog.getByRole("heading")).toHaveText(
      "用户组 当前新组 支付统计",
    );
    await page.keyboard.press("Escape");
    expect(writes).toEqual([]);
  });
  test(`original group statistics tabs footer and four actions are read only at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: string[] = [];
    let reads = 0,
      failed = false;
    const items = [
      {
        type: "alipay",
        showname: "支付宝",
        money: "90071992547409.93",
        count: "1",
        success_count: "1",
        rate: "100.00",
      },
      {
        type: "wxpay",
        showname: "微信支付",
        money: "0.01",
        count: "2",
        success_count: "1",
        rate: "50.00",
      },
      {
        type: "sandbox",
        showname: "本地模拟",
        money: "0.00",
        count: "0",
        success_count: "0",
        rate: "0.00",
      },
    ];
    await page.route("**/console/v1/**", async (route) => {
      const r = route.request(),
        u = new URL(r.url());
      if (r.method() !== "GET") writes.push(u.pathname);
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "group143",
        };
      else if (u.pathname.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          fields: [{ name: "name", label: "名称", type: "string" }],
          permissions: { create: true, update: true, delete: true },
        };
      else if (u.pathname === "/console/v1/admin/resources/groups")
        data = {
          items: [{ id: "9007199254740993", name: "列表旧名", rate_bps: "38" }],
          total: 1,
        };
      else if (u.pathname.endsWith("/reports/groups")) {
        reads++;
        expect(u.searchParams.get("gid")).toBe("9007199254740993");
        if (failed)
          return route.fulfill({
            status: 503,
            json: { code: -1, message: "本地统计暂不可用" },
          });
        data = {
          group_id: "9007199254740993",
          group_name: "服务器实际组名",
          cohort: "current_effective_group",
          timezone: "Asia/Shanghai",
          columns: [
            { key: "showname", label: "支付方式" },
            { key: "money", label: "订单金额" },
            { key: "count", label: "订单数量" },
            { key: "rate", label: "成功率" },
          ],
          days: {
            today: {
              date: "2026-10-26",
              items,
              summary: {
                type: "total",
                showname: "总计",
                money: "90071992547409.94",
                count: "3",
                success_count: "2",
                rate: "66.67",
              },
            },
            yesterday: {
              date: "2026-10-25",
              items: items.map((v) => ({
                ...v,
                money: "0.00",
                count: "0",
                success_count: "0",
                rate: "0.00",
              })),
              summary: {
                type: "total",
                showname: "总计",
                money: "0.00",
                count: "0",
                success_count: "0",
                rate: "0.00",
              },
            },
          },
        };
      }
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/group-list");
    const row = page.getByRole("row").filter({
      has: page.getByRole("cell", { name: "9007199254740993", exact: true }),
    });
    await expect(row.getByRole("button")).toHaveText(["统计", "编辑", "删除"]);
    await expect(
      row.getByRole("link", { name: "用户", exact: true }),
    ).toBeVisible();
    const opener = row.getByRole("button", { name: "统计", exact: true });
    await opener.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading")).toHaveText(
      "用户组 服务器实际组名 支付统计",
    );
    await expect(dialog.getByRole("columnheader")).toHaveText([
      "支付方式",
      "订单金额",
      "订单数量",
      "成功率",
    ]);
    await expect(dialog).toContainText("¥90071992547409.94");
    await expect(dialog).toContainText("66.67%");
    await dialog.getByRole("tab", { name: "昨日数据" }).click();
    await expect(dialog).not.toContainText("90071992547409.94");
    await expect(dialog).toContainText("2026-10-25");
    await expect(dialog.getByRole("row").last()).toContainText("0.00%");
    expect(reads).toBe(1);
    failed = true;
    await dialog.getByRole("button", { name: "刷新统计" }).click();
    await expect(dialog.getByRole("alert")).toContainText("本地统计暂不可用");
    await expect(dialog.getByRole("table")).toHaveCount(0);
    failed = false;
    await dialog.getByRole("button", { name: "刷新统计" }).click();
    await expect(dialog.getByRole("table")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    expect(writes).toEqual([]);
  });
}
