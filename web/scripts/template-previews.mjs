// Screenshot the built-in page templates for the 模板配置 cards and the 应用市场.
//
// From the repository root:
//   GOEPAY_TEMPLATE_PREVIEW_DIR=<dir> go test -run TestWriteTemplatePreviewPages ./internal/pagetemplate
//   node web/scripts/template-previews.mjs <dir> [kind ...]
//
// Writes web/public/static/template-previews/<kind>-<name>.jpg (640x360) for
// every entry in web/src/data/builtin-templates.json, or only the given kinds.
import { chromium } from "playwright-core";
import { createServer } from "node:http";
import { mkdirSync, readFileSync } from "node:fs";
import { dirname, extname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const src = process.argv[2];
if (!src) {
  console.error("usage: node web/scripts/template-previews.mjs <html dir> [kind ...]");
  process.exit(2);
}
// Optional kinds (home, pay, qrpay) limit which pictures are rewritten.
const only = new Set(process.argv.slice(3));
const web = join(dirname(fileURLToPath(import.meta.url)), "..");
const out = join(web, "public", "static", "template-previews");
mkdirSync(out, { recursive: true });
const list = JSON.parse(
  readFileSync(join(web, "src", "data", "builtin-templates.json"), "utf8"),
).filter((t) => !only.size || only.has(t.type));
// The pages are served over loopback HTTP, with /static/ from web/public, so
// the site-absolute brand images and icons load as they do on the real site.
const types = {
  ".html": "text/html; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".ico": "image/x-icon",
};
const server = createServer((req, res) => {
  const path = decodeURIComponent(new URL(req.url, "http://x").pathname);
  const root = path.startsWith("/static/") ? join(web, "public") : resolve(src);
  const file = resolve(root, "." + path);
  try {
    if (!file.startsWith(resolve(root) + sep)) throw Error("outside");
    res.writeHead(200, { "Content-Type": types[extname(file)] || "application/octet-stream" });
    res.end(readFileSync(file));
  } catch {
    res.writeHead(404).end();
  }
});
await new Promise((ok) => server.listen(0, "127.0.0.1", ok));
const base = `http://127.0.0.1:${server.address().port}`;
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
    await page.goto(`${base}/${t.type}-${t.name}.html`, { waitUntil: "load" });
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
  server.close();
}
