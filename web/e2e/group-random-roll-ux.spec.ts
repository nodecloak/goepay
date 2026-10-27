import { test, expect } from "@playwright/test";
for (const width of [1280, 375])
  test(`original random roll group editing preserves scope and rate at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes: Record<string, unknown>[] = [];
    const record = {
      id: "161",
      name: "本地命名轮询组",
      enabled: true,
      rate_bps: 0,
      info: { alipay: { rate: "99.50" } },
    };
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "random161-local",
        };
      else if (p.endsWith("/groups/schema"))
        data = {
          title: "用户组设置",
          permissions: { create: true, update: true, delete: true },
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            { name: "info", label: "支付方式分成比例", type: "json" },
          ],
        };
      else if (p.endsWith("/groups/routing-options"))
        data = {
          available: true,
          items: [{ id: "7", type: "alipay", methods: ["native"] }],
        };
      else if (p.endsWith("/pay-rolls"))
        data = {
          items: [
            {
              id: "9007199254740993",
              name: "原命名组",
              merchant_id: "0",
              type: "alipay",
              enabled: true,
              default_route: false,
              strategy: "first",
              channel_ids: "7:1",
            },
          ],
          total: 1,
        };
      else if (p.endsWith("/groups/161")) {
        if (req.method() === "PATCH") {
          const body = req.postDataJSON();
          writes.push(body);
          Object.assign(record, body);
        }
        data = record;
      } else if (p.endsWith("/groups")) data = { items: [record], total: 1 };
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/group-list");
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
        name: "编辑用户组 GID:161",
        exact: true,
      }),
      route = dialog.getByRole("combobox", { name: "支付宝通道", exact: true });
    await expect(route).toBeEnabled();
    await route.selectOption({ label: "随机可用轮询组" });
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(route).toHaveValue("default");
    await route.selectOption({ label: "随机可用轮询组" });
    await expect(
      dialog.getByLabel("支付宝分成比例（%）", { exact: true }),
    ).toHaveValue("99.50");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({
      info: { alipay: { channel: "-3", type: "roll", rate: "99.50" } },
    });
    await expect(
      page.getByRole("cell").filter({ hasText: "随机可用轮询组" }),
    ).toContainText("99.50%");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
