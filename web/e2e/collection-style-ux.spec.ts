import { test, expect, type Page } from "@playwright/test";
import { localRedBackgroundPNG } from "./local-test-images";
const assetID = "a".repeat(64),
  image = localRedBackgroundPNG();
const layout = {
  name: "original_style",
  label: "商户海报",
  enabled: true,
  sort: 2,
  url: "/collection-style-assets/" + assetID,
  img_width: 900,
  img_height: 1200,
  qr_width: 380,
  qr_height: 380,
  qr_left: 245,
  qr_top: 326,
  foreground: "#111111",
  background: "#FFFFFF",
  font: "60px '黑体'",
  font_color: "#FFFFFF",
  rec_name_left: null,
  rec_name_top: 210,
  word: 2,
};
async function setup(page: Page, area: "admin" | "merchant" = "admin") {
  const writes: { path: string; body: any }[] = [];
  await page.route("**/collection-style-assets/**", (r) =>
    r.fulfill({ contentType: "image/png", body: image }),
  );
  await page.route("**/console/v1/**", (r) => {
    const req = r.request(),
      p = new URL(req.url()).pathname;
    let data: any = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: area, role: area, admin_class: "super" },
        csrf_token: "style-csrf",
      };
    else if (p.includes("/collection-style-images/"))
      return r.fulfill({ contentType: "image/png", body: image });
    else if (p.endsWith("/collection-styles/7"))
      data = {
        id: "7",
        revision: "b".repeat(64),
        style: layout,
        fields: layout,
      };
    else if (p.endsWith("/collection-styles"))
      data = {
        items: [
          { id: "7", revision: "b".repeat(64), style: layout, fields: layout },
        ],
      };
    else if (p.endsWith("/schema"))
      data = {
        title: "聚合收款",
        fields: [{ name: "name", label: "名称", type: "string" }],
        permissions: { create: true, update: true, delete: true },
      };
    else if (p.endsWith("/link"))
      data = {
        url: "http://127.0.0.1:8186/collect/1?expires=123&token=synthetic",
        expires_at: "2026-10-26T00:00:00Z",
        layout,
        merchant_name: "本地商户",
      };
    else if (p.includes("/resources/collection-codes"))
      data = p.endsWith("/1")
        ? { id: "1", name: "原样式收款码" }
        : { items: [{ id: "1", name: "原样式收款码" }], total: 1 };
    if (req.method() !== "GET") {
      const body = p.endsWith("/collection-style-images")
        ? { multipart: true }
        : req.postData()
          ? req.postDataJSON()
          : null;
      writes.push({ path: p, body });
      expect(req.headers()["x-csrf-token"]).toBe("style-csrf");
      data = p.endsWith("/collection-style-images")
        ? { id: assetID, width: 900, height: 1200, mime: "image/png" }
        : {
            id: "7",
            revision: "c".repeat(64),
            style: { ...layout, ...(body?.style || body) },
            fields: layout,
          };
    }
    return r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  return writes;
}
for (const width of [375, 1280])
  test(`original direct style editor fields, viewport ${width}`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 });
    const writes = await setup(page);
    await page.goto("/admin/other/onecode/edit?id=7");
    for (const name of ["基本信息", "背景图", "二维码", "收款方文字"])
      await expect(
        page.getByRole("heading", { name, exact: true }),
      ).toBeVisible();
    await expect(page.getByLabel("风格标识", { exact: true })).toHaveValue(
      "original_style",
    );
    await expect(page.getByLabel("显示名称", { exact: true })).toHaveValue(
      "商户海报",
    );
    expect(writes).toHaveLength(0);
    await page.getByLabel("二维码左边距", { exact: true }).fill("200");
    await page.getByRole("button", { name: "预览", exact: true }).click();
    await expect(page.getByAltText("收款码样式预览")).toBeVisible();
    await page.evaluate(() => window.scrollTo(0, 0));
    if (width === 1280) {
      expect(
        await page
          .locator(".main-content")
          .evaluate((el) => el.getBoundingClientRect().left),
      ).toBeGreaterThanOrEqual(230);
      await expect
        .poll(
          async () =>
            await page
              .locator(".style-editor-form")
              .evaluate((el) => el.getBoundingClientRect().left),
        )
        .toBeGreaterThanOrEqual(230);
    }
    await page.screenshot({
      path: testInfo.outputPath("collection-style-viewport.png"),
      fullPage: false,
    });
    await page.screenshot({
      path: testInfo.outputPath("collection-style-editor.png"),
      fullPage: true,
    });
    if (width === 1280)
      await expect
        .poll(
          async () =>
            await page
              .locator(".style-editor-form")
              .evaluate((el) => el.getBoundingClientRect().left),
        )
        .toBeGreaterThanOrEqual(230);
    await page.getByRole("button", { name: "保存样式", exact: true }).click();
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0].body.expected_revision).toBe("b".repeat(64));
    expect(Number(writes[0].body.style.qr_left)).toBe(200);
    await expect
      .poll(() =>
        page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      )
      .toBe(true);
  });
test("merchant download consumes saved background, canvas and QR geometry", async ({
  page,
}) => {
  await setup(page, "merchant");
  await page.goto("/merchant/onecode");
  await page.getByRole("button", { name: "历史收款码", exact: true }).click();
  await page.getByRole("button", { name: "详情", exact: true }).click();
  await page.getByRole("button", { name: "生成收款链接", exact: true }).click();
  const png = page.getByAltText("聚合收款二维码");
  await expect(png).toBeVisible();
  await expect
    .poll(() => png.evaluate((el) => (el as HTMLImageElement).naturalWidth))
    .toBe(900);
  expect(
    await png.evaluate((el) => (el as HTMLImageElement).naturalHeight),
  ).toBe(1200);
  const pixels = await png.evaluate(async (el) => {
    const image = el as HTMLImageElement,
      c = document.createElement("canvas");
    c.width = image.naturalWidth;
    c.height = image.naturalHeight;
    const ctx = c.getContext("2d")!;
    ctx.drawImage(image, 0, 0);
    return {
      background: [...ctx.getImageData(0, 0, 1, 1).data],
      quietZone: [...ctx.getImageData(246, 327, 1, 1).data],
    };
  });
  expect(pixels.background).toEqual([255, 0, 0, 255]);
  expect(pixels.quietZone).toEqual([255, 255, 255, 255]);
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载二维码", exact: true }).click();
  expect((await download).suggestedFilename()).toBe("collection-code-1.png");
});
test("QR and recipient overlays move and resize the saved geometry without an implicit save", async ({
  page,
}) => {
  const writes = await setup(page);
  await page.setViewportSize({ width: 1280, height: 1000 });
  await page.goto("/admin/other/onecode/edit?id=7");
  await page.getByRole("button", { name: "预览", exact: true }).click();
  await expect(page.getByAltText("收款码样式预览")).toBeVisible();
  const qr = page.getByRole("button", { name: "拖动二维码", exact: true });
  await qr.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByLabel("二维码左边距", { exact: true })).toHaveValue(
    "246",
  );
  await page
    .getByRole("button", { name: "拖动收款方文字", exact: true })
    .focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByLabel("文字横坐标", { exact: true })).toHaveValue(
    "451",
  );
  const resize = page.getByRole("button", { name: "缩放二维码", exact: true });
  await expect(resize).toBeEnabled();
  const box = await resize.boundingBox();
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
  await page.mouse.down();
  await page.mouse.move(
    box!.x + box!.width / 2 + 20,
    box!.y + box!.height / 2 + 20,
  );
  await page.mouse.up();
  await expect
    .poll(async () =>
      Number(await page.getByLabel("二维码宽度", { exact: true }).inputValue()),
    )
    .toBeGreaterThan(380);
  expect(
    await page.getByLabel("二维码宽度", { exact: true }).inputValue(),
  ).toBe(await page.getByLabel("二维码高度", { exact: true }).inputValue());
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "保存样式", exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0].body.style.rec_name_left).toBe("451");
});
test("invalid geometry and ambiguous IDs never produce a style write", async ({
  page,
}) => {
  const writes = await setup(page);
  await page.goto("/admin/other/onecode/edit?id=7");
  await page.getByLabel("二维码左边距", { exact: true }).fill("899");
  await page.getByRole("button", { name: "保存样式", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("超出画布");
  expect(writes).toHaveLength(0);
  await page.goto("/admin/other/onecode/edit?id=7&id=8");
  await expect(page.getByRole("alert")).toContainText("样式ID无效");
  await expect(
    page.getByRole("button", { name: "保存样式", exact: true }),
  ).toBeDisabled();
  expect(writes).toHaveLength(0);
});
test("local image upload remains a draft until explicit style save", async ({
  page,
}) => {
  const writes = await setup(page);
  await page.goto("/admin/other/onecode/edit");
  await page.getByLabel("上传背景图", { exact: true }).setInputFiles({
    name: "background.png",
    mimeType: "image/png",
    buffer: image,
  });
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0].path).toBe("/console/v1/admin/collection-style-images");
  await expect(page.getByLabel("图片地址", { exact: true })).toHaveValue(
    "/collection-style-assets/" + assetID,
  );
  await page.getByLabel("风格标识", { exact: true }).fill("new_style");
  await page.getByLabel("显示名称", { exact: true }).fill("本地上传样式");
  await page.getByRole("button", { name: "保存样式", exact: true }).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1].path).toBe("/console/v1/admin/collection-styles");
  expect(writes[1].body.url).toBe("/collection-style-assets/" + assetID);
});
