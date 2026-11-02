import { test, expect, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { localCrudFixture } from "./local-crud-fixture";

test.describe("real local system management workflows229", () => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1",
    "Owned loopback simulation only",
  );
  test.setTimeout(90000);
  let local: Awaited<ReturnType<typeof localCrudFixture>>;
  test.beforeAll(async ({ playwright }) => {
    local = await localCrudFixture(playwright, import.meta.url);
  });
  test.afterAll(async () => {
    await local?.dispose();
  });
  function sql(query: string) {
    const database = process.env.GOEPAY_LIVE_OWNED_DATABASE || "";
    expect(database).toMatch(/^system229_[a-f0-9]{32}$/);
    const source = new URL(local.env.DATABASE_URL);
    expect(source.hostname).toBe("127.0.0.1");
    expect(source.port).toBe("55101");
    expect(source.pathname).toBe("/" + database);
    return execFileSync(
      "docker",
      [
        "exec",
        "-i",
        "goepay-full195-windows-b378c4a3",
        "psql",
        "-X",
        "-U",
        "goepay",
        "-d",
        database,
        "-At",
        "-v",
        "ON_ERROR_STOP=1",
      ],
      { input: query + ";\n", encoding: "utf8" },
    ).trim();
  }
  function dateAgo(days: number) {
    return new Date(Date.now() + 8 * 3600000 - days * 86400000)
      .toISOString()
      .slice(0, 10);
  }
  async function attach(page: Page, width: number) {
    await local.attach(page, "admin");
    await page.setViewportSize({ width, height: 1100 });
  }
  for (const width of [375, 1440]) {
    test(`administrator create, cancel role change, login, disable and delete at ${width}px`, async ({
      page,
      playwright,
    }) => {
      await attach(page, width);
      const username = local.tag + "-admin-" + width,
        password = "synthetic-" + randomUUID();
      await page.goto("/admin/system/admin-user");
      await page
        .getByRole("button", { name: "添加管理员", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      await dialog.getByRole("textbox", { name: /账号$/ }).fill(username);
      await dialog
        .getByLabel("名称", { exact: false })
        .fill("Synthetic administrator229");
      await dialog.getByLabel("密码", { exact: false }).fill(password);
      await dialog
        .getByRole("combobox", { name: /权限等级/ })
        .selectOption("operator");
      await dialog
        .getByRole("checkbox", { name: "订单管理", exact: true })
        .check();
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      const row = page
        .getByRole("row")
        .filter({
          has: page.getByRole("cell", { name: username, exact: true }),
        });
      await expect(row).toBeVisible();
      const saved = (
        await local.call("admin", "/resources/admin-users?limit=100")
      ).items.find((r: any) => r.username === username);
      expect(saved.permissions).toEqual(["OrderList"]);
      await row.getByRole("button", { name: "编辑", exact: true }).click();
      await dialog
        .getByRole("combobox", { name: /权限等级/ })
        .selectOption("super");
      await expect(dialog.locator(".admin-permission-tree")).toHaveCount(0);
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      expect(
        (await local.call("admin", "/resources/admin-users/" + saved.id))
          .admin_class,
      ).toBe("operator");
      const actor = await playwright.request.newContext({
        baseURL: local.baseURL,
      });
      try {
        const login = await actor.post("/console/v1/admin/login", {
          data: { username, password },
        });
        expect(login.ok()).toBeTruthy();
        expect(
          (await (await actor.get("/console/v1/admin/session")).json()).data
            .user.permissions,
        ).toEqual(["OrderList"]);
        expect(
          (await actor.get("/console/v1/admin/resources/admin-users")).status(),
        ).toBe(403);
        await row.getByRole("button", { name: "编辑", exact: true }).click();
        await dialog
          .getByRole("checkbox", { name: "启用", exact: true })
          .uncheck();
        await dialog.getByRole("button", { name: "保存", exact: true }).click();
        await expect(dialog).toHaveCount(0);
        expect(
          (await local.call("admin", "/resources/admin-users/" + saved.id))
            .enabled,
        ).toBe(false);
        expect((await actor.get("/console/v1/admin/session")).status()).toBe(
          401,
        );
      } finally {
        await actor.dispose();
      }
      await row.getByRole("button", { name: "删除", exact: true }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "确认删除", exact: true })
        .click();
      await expect(row).toHaveCount(0);
      await page.reload();
      await expect(row).toHaveCount(0);
    });

    test(`announcement add, edit/cancel, visibility, reload and delete at ${width}px`, async ({
      page,
    }) => {
      await attach(page, width);
      const text = local.tag + "-announcement-" + width;
      await page.goto("/admin/system/anounce");
      await page.getByRole("button", { name: "添加公告", exact: true }).click();
      const add = page.getByRole("region", { name: "添加公告", exact: true });
      await add
        .getByRole("textbox", { name: "公告内容", exact: true })
        .fill(text);
      await add.getByRole("textbox", { name: "排序", exact: true }).fill("29");
      await add
        .getByRole("textbox", { name: "公告文字颜色", exact: true })
        .fill("#123456");
      const created = page.waitForResponse(
        (r) =>
          r.request().method() === "POST" &&
          new URL(r.url()).pathname.endsWith("/resources/announcements"),
      );
      await add.getByRole("button", { name: "添加", exact: true }).click();
      const id = (await (await created).json()).data.id;
      const row = page.locator(".announcement-row").filter({ hasText: text });
      await expect(row).toBeVisible();
      await row.getByRole("button", { name: "编辑", exact: true }).click();
      const edit = page.getByRole("region", { name: "编辑公告", exact: true });
      await edit.getByRole("textbox", { name: "排序", exact: true }).fill("10");
      await edit.getByRole("button", { name: "取消编辑", exact: true }).click();
      expect(
        String(
          (await local.call("admin", "/resources/announcements/" + id)).sort,
        ),
      ).toBe("29");
      await row.getByRole("button", { name: "编辑", exact: true }).click();
      await edit.getByRole("textbox", { name: "排序", exact: true }).fill("10");
      await edit
        .getByRole("textbox", { name: "公告文字颜色", exact: true })
        .fill("#654321");
      await edit.getByRole("button", { name: "保存", exact: true }).click();
      await expect(edit).toHaveCount(0);
      await row.getByRole("button", { name: "隐藏", exact: true }).click();
      await expect(row).toContainText("已隐藏");
      await page.reload();
      await expect(row).toContainText("已隐藏");
      expect(
        await local.call("admin", "/resources/announcements/" + id),
      ).toMatchObject({ sort: "10", color: "#654321", enabled: false });
      await row.getByRole("button", { name: "显示", exact: true }).click();
      await expect(row).toContainText("显示中");
      await row.getByRole("button", { name: "删除", exact: true }).click();
      await page
        .getByRole("dialog", { name: "删除公告" })
        .getByRole("button", { name: "取消", exact: true })
        .click();
      await expect(row).toBeVisible();
      await row.getByRole("button", { name: "删除", exact: true }).click();
      await page
        .getByRole("dialog", { name: "删除公告" })
        .getByRole("button", { name: "确定删除", exact: true })
        .click();
      await expect(row).toHaveCount(0);
      expect(
        (
          await local.request("admin", "/resources/announcements/" + id)
        ).status(),
      ).toBe(404);
    });

    test(`one-time task configuration, cancel and pending-only manual run at ${width}px`, async ({
      page,
    }) => {
      await attach(page, width);
      const key = width === 375 ? "order" : "check";
      const original = await local.call("admin", "/scheduled-tasks/" + key);
      await page.goto("/admin/system/cron");
      const card = page
        .locator(".job-card")
        .filter({
          has: page.locator(".job-key").getByText(key, { exact: true }),
        });
      await expect(card).toContainText(original.description);
      await card.getByRole("button", { name: "配置", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await dialog
        .getByRole("button", { name: "执行频率", exact: true })
        .click();
      await page.getByRole("option", { name: "分钟", exact: true }).click();
      await dialog.getByLabel("间隔", { exact: true }).fill("30");
      await dialog
        .getByRole("button", { name: "执行频率", exact: true })
        .click();
      await page.getByRole("option", { name: "仅一次", exact: true }).click();
      await expect(dialog.locator("#scheduler171-interval")).toHaveCount(0);
      const future = new Date(Date.now() + 8 * 3600000 + 3600000)
        .toISOString()
        .slice(0, 19);
      await dialog
        .getByLabel("执行时间（UTC+8）", { exact: true })
        .fill(future);
      const toggle = dialog.getByRole("switch", {
        name: "开启此任务",
        exact: true,
      });
      if ((await toggle.getAttribute("aria-checked")) !== "true")
        await toggle.click();
      await dialog
        .getByRole("button", { name: "核对配置", exact: true })
        .click();
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      expect(
        (await local.call("admin", "/scheduled-tasks/" + key)).fingerprint,
      ).toBe(original.fingerprint);
      await card.getByRole("button", { name: "配置", exact: true }).click();
      await dialog
        .getByRole("button", { name: "执行频率", exact: true })
        .click();
      await page.getByRole("option", { name: "仅一次", exact: true }).click();
      await dialog
        .getByLabel("执行时间（UTC+8）", { exact: true })
        .fill(future);
      if ((await toggle.getAttribute("aria-checked")) !== "true")
        await toggle.click();
      await dialog
        .getByRole("button", { name: "核对配置", exact: true })
        .click();
      await dialog
        .getByRole("button", { name: "确认保存", exact: true })
        .click();
      await expect(dialog).toContainText("任务配置已保存");
      const saved = await local.call("admin", "/scheduled-tasks/" + key);
      expect(saved.schedule.frequency).toBe("once");
      expect(saved.schedule.interval).toBe(1);
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      await page.reload();
      await card.getByRole("button", { name: "立即运行", exact: true }).click();
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      expect(
        (
          await local.call(
            "admin",
            "/scheduled-tasks/" + key + "/runs?limit=20&before_id=0",
          )
        ).items,
      ).toHaveLength(0);
      await card.getByRole("button", { name: "立即运行", exact: true }).click();
      await dialog
        .getByRole("button", { name: "确认安排", exact: true })
        .click();
      await expect(dialog).toContainText("等待调度器执行");
      const history = await local.call(
        "admin",
        "/scheduled-tasks/" + key + "/runs?limit=20&before_id=0",
      );
      expect(history.items).toHaveLength(1);
      expect(history.items[0].status).toBe("pending");
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      await card.getByRole("button", { name: "执行记录", exact: true }).click();
      await expect(dialog).toContainText("待运行");
      await expect(dialog).not.toContainText("本轮已完成");
    });

    test(`login and runtime log filters query and reset actual persisted records at ${width}px`, async ({
      page,
    }) => {
      await attach(page, width);
      const tag = local.tag + "-logs-" + width;
      expect(tag).toMatch(/^[a-z0-9-]+$/);
      sql(
        `INSERT INTO console_login_audit(username,role,ip,success) VALUES('${tag}','admin','127.0.0.229',true); INSERT INTO runtime_events(level,error_code,content) VALUES('info','SYNTHETIC229','${tag}')`,
      );
      await page.goto("/admin/system/log");
      await page.getByRole("button", { name: "搜索字段", exact: true }).click();
      await page.getByRole("option", { name: "操作IP", exact: true }).click();
      await page
        .getByRole("textbox", { name: "搜索内容", exact: true })
        .fill("127.0.0.229");
      await page.getByRole("button", { name: "查询", exact: true }).click();
      await expect(
        page.getByRole("cell", { name: "127.0.0.229", exact: true }).first(),
      ).toBeVisible();
      await page.getByRole("button", { name: "重置", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "搜索内容", exact: true }),
      ).toHaveValue("");
      await page.goto("/admin/system/runtime-log");
      await page.getByLabel("日期", { exact: true }).fill(dateAgo(0));
      await page.getByRole("button", { name: "查询", exact: true }).click();
      await expect(
        page.getByRole("cell", { name: tag, exact: true }),
      ).toBeVisible();
      await page.getByRole("button", { name: "重置", exact: true }).click();
      await expect(page.getByLabel("日期", { exact: true })).toHaveValue(
        dateAgo(0),
      );
    });

    test(`cleanup previews four counts, cancels, removes old login data and archives only old runtime events at ${width}px`, async ({
      page,
    }) => {
      await attach(page, width);
      const tag = local.tag + "-cleanup-" + width;
      expect(tag).toMatch(/^[a-z0-9-]+$/);
      sql(
        `INSERT INTO console_login_audit(username,role,ip,success,created_at) VALUES('${tag}-old','admin','127.0.0.229',true,now()-interval '40 days'),('${tag}-fresh','admin','127.0.0.229',true,now()); INSERT INTO runtime_events(level,error_code,content,created_at) VALUES('info','SYNTHETIC229','${tag}-old',now()-interval '40 days'),('info','SYNTHETIC229','${tag}-fresh',now())`,
      );
      await page.goto("/admin/system/clean");
      await page
        .getByLabel("清理截止日期（UTC+08:00）", { exact: true })
        .fill(dateAgo(30));
      await page
        .getByRole("button", { name: "预览清理范围", exact: true })
        .click();
      for (const key of [
        "expired_login_audit_rows",
        "expired_sessions",
        "expired_attempts",
        "expired_login_throttle_keys",
      ])
        await expect(
          page.locator(`[data-cleanup-count="${key}"]`),
        ).toBeVisible();
      expect(
        Number(
          await page
            .locator('[data-cleanup-count="expired_login_audit_rows"]')
            .textContent(),
        ),
      ).toBeGreaterThanOrEqual(1);
      await page.getByRole("button", { name: "立即删除", exact: true }).click();
      await page
        .getByRole("dialog", { name: "确认清理登录数据", exact: true })
        .getByRole("button", { name: "取消", exact: true })
        .click();
      expect(
        sql(
          `SELECT count(*) FROM console_login_audit WHERE username='${tag}-old'`,
        ),
      ).toBe("1");
      await page.getByRole("button", { name: "立即删除", exact: true }).click();
      await page
        .getByRole("dialog", { name: "确认清理登录数据", exact: true })
        .getByRole("button", { name: "确认清理", exact: true })
        .click();
      await expect(page.getByRole("status")).toContainText("已清理：登录日志");
      expect(
        sql(
          `SELECT count(*) FROM console_login_audit WHERE username='${tag}-old'`,
        ),
      ).toBe("0");
      expect(
        sql(
          `SELECT count(*) FROM console_login_audit WHERE username='${tag}-fresh'`,
        ),
      ).toBe("1");
      await page
        .getByRole("button", { name: "清理运行日志", exact: true })
        .click();
      const dialog = page.getByRole("dialog", {
        name: "清除运行日志",
        exact: true,
      });
      await dialog.getByLabel("保留天数", { exact: true }).fill("30");
      await dialog
        .getByRole("button", { name: "预览清除范围", exact: true })
        .click();
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      expect(
        sql(
          `SELECT count(*) FROM runtime_events WHERE content='${tag}-old' AND archived_at IS NULL`,
        ),
      ).toBe("1");
      await page
        .getByRole("button", { name: "清理运行日志", exact: true })
        .click();
      await dialog
        .getByRole("button", { name: "预览清除范围", exact: true })
        .click();
      await dialog
        .getByRole("button", { name: "确认清除", exact: true })
        .click();
      await expect(dialog.getByRole("status")).toBeVisible();
      expect(
        sql(
          `SELECT count(*) FROM runtime_events WHERE content='${tag}-old' AND archived_at IS NOT NULL`,
        ),
      ).toBe("1");
      expect(
        sql(
          `SELECT count(*) FROM runtime_events WHERE content='${tag}-fresh' AND archived_at IS NULL`,
        ),
      ).toBe("1");
    });

    test(`unconfigured update source shows current version and never offers a fake upgrade at ${width}px`, async ({
      page,
    }) => {
      await attach(page, width);
      const writes: string[] = [];
      page.on("request", (r) => {
        if (
          r.method() !== "GET" &&
          new URL(r.url()).pathname.includes("/system-update")
        )
          writes.push(r.method());
      });
      await page.goto("/admin/system/update");
      const panel = page.getByRole("region", { name: "系统更新", exact: true });
      await expect(panel.getByText("当前版本", { exact: true })).toBeVisible();
      await expect(panel.getByText("最新版本", { exact: true })).toBeVisible();
      await expect(
        panel.getByRole("button", { name: "准备更新包", exact: true }),
      ).toHaveCount(0);
      await panel
        .getByRole("button", { name: "重新检测", exact: true })
        .click();
      await expect(panel.getByText("当前版本", { exact: true })).toBeVisible();
      expect(writes).toHaveLength(0);
      expect((await local.call("admin", "/system-update")).check_status).toBe(
        "unconfigured",
      );
    });
  }
});
