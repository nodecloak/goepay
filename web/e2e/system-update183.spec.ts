import { test, expect, type Page } from "@playwright/test";
import actual from "./systemupdate183-http-fixture.json" with { type: "json" };
async function setup(page: Page, loss = false, applied = false) {
  let prepares = 0,
    reads = 0,
    submitted = false;
  await page.route("**/console/v1/**", async (route) => {
    const q = route.request(),
      path = new URL(q.url()).pathname;
    let data: unknown = { items: [], total: 0 };
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "183",
          role: "admin",
          admin_class: "super",
          name: "合成管理员",
        },
        csrf_token: "synthetic183-csrf",
      };
    else if (path === "/console/v1/public/layout")
      data = { zhuce: "", footer: "", format: "html" };
    else if (path === "/console/v1/admin/system-update")
      data = applied
        ? {
            ...actual.available,
            can_prepare: false,
            need_update: false,
            check_status: "current",
            current_version: actual.applied.target!.version,
            current_source: actual.applied.target!.source_sha256,
            preparation: actual.applied,
          }
        : submitted
          ? {
              ...actual.available,
              can_prepare: false,
              preparation: actual.prepared,
            }
          : actual.available;
    else if (path.endsWith("/system-update/prepare")) {
      prepares++;
      submitted = true;
      expect(q.headers()["x-csrf-token"]).toBe("synthetic183-csrf");
      expect(q.postDataJSON()).toEqual({
        intent: actual.available.fingerprint,
        expected_fingerprint: actual.available.fingerprint,
      });
      if (loss && prepares === 1) return route.abort("failed");
      data = actual.accepted;
    } else if (path.includes("/system-update/preparations/")) {
      reads++;
      data = applied ? actual.applied : actual.prepared;
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, message: "", data }),
    });
  });
  return () => ({ prepares, reads });
}
for (const width of [1440, 375])
  test(`original system update version, confirmation and actual preparation ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const calls = await setup(page);
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto("/admin/system/update");
    await expect(page.getByText("当前版本", { exact: true })).toBeVisible();
    await expect(
      page.getByText("<script>literal changelog</script>", { exact: false }),
    ).toBeVisible();
    expect(await page.locator(".update-record script").count()).toBe(0);
    await page.getByRole("button", { name: "准备更新包", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "确认准备更新包",
      exact: true,
    });
    await expect(dialog).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "确认", exact: true }),
    ).toBeDisabled();
    expect(calls().prepares).toBe(0);
    await dialog.getByRole("checkbox").check();
    await page.screenshot({
      path: test.info().outputPath(`original-system-update183-${width}.png`),
      fullPage: true,
    });
    await dialog.getByRole("button", { name: "确认", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "准备完成，等待部署", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("更新已部署", { exact: true })).toHaveCount(0);
    expect(calls().prepares).toBe(1);
    expect(calls().reads).toBeGreaterThan(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
  });
test("a lost preparation response requires original state reads and never a second preparation", async ({
  page,
}) => {
  const calls = await setup(page, true);
  await page.goto("/admin/system/update");
  await page.getByRole("button", { name: "准备更新包", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "确认", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "核对原更新结果", exact: true }),
  ).toBeVisible();
  expect(calls().prepares).toBe(1);
  await page
    .getByRole("button", { name: "核对原更新结果", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "准备完成，等待部署", exact: true }),
  ).toBeVisible();
  expect(calls().prepares).toBe(1);
  expect(calls().reads).toBeGreaterThan(0);
});
test("a verified restarted build displays the actual applied result and completed progress", async ({
  page,
}) => {
  await setup(page, false, true);
  await page.goto("/admin/system/update");
  await expect(
    page.getByRole("heading", { name: "更新已部署", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "准备更新包", exact: true }),
  ).toHaveCount(0);
  const progress = page.locator(".actual-progress progress");
  await expect(progress).toHaveCount(2);
  expect(await progress.nth(0).getAttribute("value")).toBe(
    String(actual.applied.progress.total_bytes),
  );
  expect(await progress.nth(1).getAttribute("value")).toBe(
    String(actual.applied.progress.total_files),
  );
});
