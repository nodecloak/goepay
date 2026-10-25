import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

const root = path.dirname(fileURLToPath(import.meta.url));
const args = process.argv.slice(2);
if (args.length !== 2 || args[0] !== '--output' || !path.isAbsolute(args[1])) throw new Error('Usage: node build.mjs --output <new absolute directory>');
const output = path.resolve(args[1]);
if (fs.existsSync(output) || !fs.statSync(path.dirname(output)).isDirectory()) throw new Error('A new output directory with an existing parent is required');
// Construct the entire reviewed file set before writing the new directory.
const source = name => fs.readFileSync(path.join(root, name), 'utf8');
const files = [];
function add(name, content) { files.push({ name, content }); }
for (const provider of ['wxpay', 'alipay']) {
  const wx = provider === 'wxpay';
  const suffix = wx ? 'wxml' : 'axml';
  const style = wx ? 'wxss' : 'acss';
  const app = { pages: ['pages/pay/pay'], window: wx ? { navigationBarTitleText: '订单支付', navigationBarBackgroundColor: '#ffffff', navigationBarTextStyle: 'black', backgroundColor: '#f5f7f9' } : { defaultTitle: '订单支付', titleBarColor: '#ffffff', backgroundColor: '#f5f7f9' } };
  add(`${provider}/app.json`, JSON.stringify(app, null, 2) + '\n');
  add(`${provider}/app.js`, "'use strict';\nApp({});\n");
  add(`${provider}/config.js`, "'use strict';\n// Public deployment settings only. No payment keys belong in this project.\nmodule.exports = Object.freeze({ enabled: false, origin: 'https://pay.example.invalid', appId: 'UNCONFIGURED', channelIDs: Object.freeze(['7']) });\n");
  add(`${provider}/utils/payment-client.js`, source('src/payment-client.cjs'));
  add(`${provider}/utils/page.js`, source('src/page.cjs').split("require('./payment-client.cjs')").join("require('./payment-client.js')"));
  add(`${provider}/pages/pay/pay.js`, "'use strict';\nconst { createPage } = require('../../utils/page');\nconst config = require('../../config');\nPage(createPage('" + provider + "', " + (wx ? 'wx' : 'my') + ", config));\n");
  add(`${provider}/pages/pay/pay.${suffix}`, source(`templates/pay.${suffix}`));
  add(`${provider}/pages/pay/pay.${style}`, source('templates/pay.css'));
  add(`${provider}/pages/pay/pay.json`, '{}\n');
  if (wx) add(`${provider}/project.config.json`, JSON.stringify({ appid: 'touristappid', projectname: 'goepay-mini-client', compileType: 'miniprogram', miniprogramRoot: './', setting: { es6: true, minified: false } }, null, 2) + '\n');
  else add(`${provider}/mini.project.json`, JSON.stringify({ miniprogramRoot: './', component2: true }, null, 2) + '\n');
}
files.sort((a, b) => a.name.localeCompare(b.name, 'en'));
const manifest = { schema: 'goepay-mini-client-v1', default_external_io_enabled: false, files: files.map(f => ({ path: f.name, sha256: createHash('sha256').update(f.content).digest('hex') })) };
fs.mkdirSync(output);
for (const file of files) { const target = path.join(output, file.name); fs.mkdirSync(path.dirname(target), { recursive: true }); fs.writeFileSync(target, file.content, { flag: 'wx' }); }
fs.writeFileSync(path.join(output, 'MANIFEST.json'), JSON.stringify(manifest, null, 2) + '\n', { flag: 'wx' });
console.log(JSON.stringify({ output, projects: 2, files: files.length, external_io_enabled: false }));
