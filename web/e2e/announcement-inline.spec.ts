import { test, expect } from "@playwright/test";
for (const width of [375, 1280])
  test(`inline announcement editor preserves its draft and saves once at ${width}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    let writes = 0,
      reads = 0,
      release!: () => void;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    const rows = [
      {
        id: "1",
        content: "Existing announcement",
        content_format: "text",
        enabled: true,
        sort: 50,
        color: "",
        created_at: "2026-11-01T01:02:03Z",
      },
    ];
    await page.route("**/console/v1/**", async (route) => {
      const req = route.request(),
        path = new URL(req.url()).pathname;
      const ok = (data: unknown) => route.fulfill({ json: { code: 0, data } });
      if (path.endsWith("/session"))
        return ok({
          user: {
            id: "1",
            role: "admin",
            admin_class: "super",
            name: "Synthetic",
          },
          csrf_token: "synthetic",
        });
      if (path.endsWith("/resources/announcements/schema"))
        return ok({
          fields: [],
          permissions: { create: true, update: true, delete: true },
        });
      if (path.endsWith("/resources/announcements")) {
        if (req.method() === "POST") {
          writes++;
          const body = req.postDataJSON();
          expect(body.sort).toBe(8);
          expect(body.content_format).toBe("html");
          await held;
          rows.push({ ...body, id: "2", created_at: "2026-11-01T01:03:04Z" });
          return ok(rows[1]);
        }
        reads++;
        return ok({ items: rows, total: rows.length });
      }
      return ok({ items: [], total: 0 });
    });
    await page.goto("/admin/system/anounce");
    const editor = page.getByRole("region", { name: "添加公告", exact: true });
    await expect(editor).toBeVisible();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await editor
      .locator('[contenteditable="true"]')
      .fill("Synthetic inline announcement");
    await editor.getByRole("textbox", { name: "排序", exact: true }).fill("8");
    await page.getByRole("button", { name: "添加公告", exact: true }).click();
    await expect(
      editor.getByRole("textbox", { name: "排序", exact: true }),
    ).toHaveValue("8");
    const save = editor.locator('button[type="submit"]');
    await save.click();
    await expect(save).toBeDisabled();
    await expect.poll(() => writes).toBe(1);
    expect(writes).toBe(1);
    release();
    await expect(page.locator('[aria-label="已发公告"]')).toContainText(
      "Synthetic inline announcement",
    );
    expect(writes).toBe(1);
    expect(reads).toBe(2);
    await page.screenshot({
      path: testInfo.outputPath(`announcement-inline-${width}.png`),
      fullPage: true,
    });
  });
