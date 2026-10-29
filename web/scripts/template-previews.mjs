// Screenshot the built-in page templates for the 模板配置 cards and the 应用市场.
//
// From the repository root:
//   GOEPAY_TEMPLATE_PREVIEW_DIR=<dir> go test -run TestWriteTemplatePreviewPages ./internal/pagetemplate
//   node web/scripts/template-previews.mjs <dir>
//
// Writes web/public/static/template-previews/<kind>-<name>.jpg (640x360) for
// every entry in web/src/data/builtin-templates.json.
import { chromium } from "playwright-core";
import { mkdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const src = process.argv[2];
if (!src) {
  console.error("usage: node web/scripts/template-previews.mjs <html dir>");
  process.exit(2);
}
const web = join(dirname(fileURLToPath(import.meta.url)), "..");
const out = join(web, "public", "static", "template-previews");
mkdirSync(out, { recursive: true });
const list = JSON.parse(
  readFileSync(join(web, "src", "data", "builtin-templates.json"), "utf8"),
);
const browser = await chromium.launch();
try {
  for (const t of list) {
    // Home pages are wide landing pages; pay/qrpay pages are a centred card.
    const width = t.type === "home" ? 1280 : 960;
    const page = await browser.newPage({
      viewport: { width, height: Math.round((width * 9) / 16) },
      deviceScaleFactor: 640 / width,
      locale: "zh-CN",
      reducedMotion: "reduce",
    });
    await page.goto(pathToFileURL(join(src, `${t.type}-${t.name}.html`)).href);
    await page.screenshot({
      path: join(out, `${t.type}-${t.name}.jpg`),
      type: "jpeg",
      quality: 82,
    });
    await page.close();
    console.log(`${t.type}-${t.name}.jpg`);
  }
} finally {
  await browser.close();
}
