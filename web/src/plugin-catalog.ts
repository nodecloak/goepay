// Plugin catalog for the 应用市场 and 本地插件 pages.
//
// The original console downloads PHP plugins from a remote market. This Go
// build cannot load PHP code: its plugins are compiled into the service. The
// catalog therefore lists the original market (captured from the epay_pro
// app store) and marks the entries this build runs natively.
export type PluginType =
  | "payment"
  | "profitsharing"
  | "complain"
  | "applyments"
  | "sms"
  | "mail"
  | "addons"
  | "template";

/** Category tabs, in the original console's order. */
export const pluginTypes: readonly (readonly [PluginType, string])[] = [
  ["payment", "支付插件"],
  ["profitsharing", "分账插件"],
  ["complain", "投诉插件"],
  ["applyments", "进件插件"],
  ["sms", "短信插件"],
  ["mail", "邮件插件"],
  ["addons", "功能扩展"],
  ["template", "模板"],
];

export const templateKinds: Readonly<Record<string, string>> = {
  home: "首页模板",
  pay: "支付页模板",
  qrpay: "聚合收款页模板",
};

export interface CatalogEntry {
  type: PluginType;
  /** Template kind (home, pay, qrpay); empty for other types. */
  sub?: string;
  name: string;
  title: string;
  desc: string;
  author: string;
  version: string;
  price: string;
  pay: string[];
  trans: string[];
  link?: string;
  icon?: string;
}

export interface BuiltinPlugin {
  /** What the Go build provides for this entry. */
  note: string;
}

// Payment plugins the Go build actually serves, derived from the Go plugin
// registry by internal/adapters/payment/ported_catalog_test.go (run it with
// GOEPAY_UPDATE_PLUGIN_CATALOG=1 to regenerate). This is the single source of
// truth for which payment plugins are 已内置, so the console can never claim a
// plugin that the service does not run, and every ported plugin shows its
// original title/author/pay types without a second hand-maintained list.
import portedPayments from "./data/ported-plugins.json";

export interface PortedPlugin {
  name: string;
  title: string;
  author: string;
  types: string[];
  trans: string[];
  note: string;
}

/** The ported payment plugins, keyed by catalog key (payment:<name>). */
export const portedPaymentPlugins: Readonly<Record<string, PortedPlugin>> =
  Object.fromEntries(
    (portedPayments as PortedPlugin[]).map((p) => [`payment:${p.name}`, p]),
  );

// Page templates the Go build serves, generated from internal/pagetemplate by
// catalog_golden_test.go (GOEPAY_UPDATE_PLUGIN_CATALOG=1 regenerates it). They
// all ship with the service, so the market shows each one as installed.
import builtinTemplateData from "./data/builtin-templates.json";

export interface BuiltinTemplate {
  type: string;
  name: string;
  title: string;
  description: string;
}

export const builtinTemplates = builtinTemplateData as BuiltinTemplate[];

function templateNote(t: BuiltinTemplate): string {
  return t.type === "home" && t.name !== "minimal"
    ? "Go 版按原模板的内容结构重做，随服务内置、默认已安装，在“模板配置 › 首页”中选用。"
    : "Go 版内置模板，随服务内置、默认已安装，在“模板配置”中选用。";
}

// Non-payment entries this Go build implements (profit sharing, mail).
// Payment plugins come from portedPaymentPlugins above and templates from
// builtinTemplates; keep the native alipay/wxpay notes here too so they
// survive even if the generated catalog is unavailable.
const nonPaymentBuiltins: Readonly<Record<string, BuiltinPlugin>> = {
  "profitsharing:alipay": { note: "Go 版内置实现，在“分账管理”中使用。" },
  "profitsharing:wxpay": { note: "Go 版内置实现，在“分账管理”中使用。" },
  "mail:smtp": { note: "Go 版内置实现，在“系统设置 › 邮箱配置”中使用。" },
  ...Object.fromEntries(
    builtinTemplates.map((t) => [
      `template:${t.type}:${t.name}`,
      { note: templateNote(t) },
    ]),
  ),
};

export const builtinPlugins: Readonly<Record<string, BuiltinPlugin>> = {
  ...nonPaymentBuiltins,
  ...Object.fromEntries(
    Object.entries(portedPaymentPlugins).map(([key, p]) => [
      key,
      { note: p.note },
    ]),
  ),
};

export function catalogKey(entry: Pick<CatalogEntry, "type" | "name" | "sub">) {
  return entry.type === "template"
    ? `template:${entry.sub ?? ""}:${entry.name}`
    : `${entry.type}:${entry.name}`;
}

export function builtinPlugin(
  entry: Pick<CatalogEntry, "type" | "name" | "sub">,
): BuiltinPlugin | undefined {
  return builtinPlugins[catalogKey(entry)];
}

export function priceLabel(price: string): string {
  return !price || Number(price) === 0 ? "免费" : `¥${price}`;
}

/** Entries of one category whose title or identifier contains the query. */
export function filterCatalog(
  items: readonly CatalogEntry[],
  type: PluginType,
  query: string,
): CatalogEntry[] {
  const q = query.trim().toLowerCase();
  return items.filter(
    (item) =>
      item.type === type &&
      (!q ||
        item.title.toLowerCase().includes(q) ||
        item.name.toLowerCase().includes(q)),
  );
}

/** The built-in plugins of one category (本地插件), catalog details joined. */
export function localPlugins(
  items: readonly CatalogEntry[],
  type: PluginType,
  query: string,
): CatalogEntry[] {
  return filterCatalog(items, type, query).filter((item) =>
    builtinPlugin(item),
  );
}

export function builtinCount(items: readonly CatalogEntry[]): number {
  return items.filter((item) => builtinPlugin(item)).length;
}

const templateKindOrder: Readonly<Record<string, number>> = {
  home: 0,
  pay: 1,
  qrpay: 2,
};
function templateRank(name: string): number {
  if (name === "default") return 0;
  const m = /^index(\d+)$/.exec(name);
  return m ? Number(m[1]) : 100;
}

/**
 * The market list plus the built-in templates the original market does not
 * have (GOEPAY's own styles). Templates are grouped by kind, then default,
 * index1 … index10, then the rest.
 */
export function withBuiltinTemplates(
  items: readonly CatalogEntry[],
): CatalogEntry[] {
  const have = new Set(items.map((item) => catalogKey(item)));
  const extra: CatalogEntry[] = builtinTemplates
    .filter((t) => !have.has(`template:${t.type}:${t.name}`))
    .map((t) => ({
      type: "template",
      sub: t.type,
      name: t.name,
      title: t.title,
      desc: t.description,
      author: "GOEPAY",
      version: "1.0",
      price: "0.00",
      pay: [],
      trans: [],
      icon: "/static/icon/category/template.png",
    }));
  const templates = [
    ...items.filter((item) => item.type === "template"),
    ...extra,
  ].sort(
    (a, b) =>
      (templateKindOrder[a.sub ?? ""] ?? 9) -
        (templateKindOrder[b.sub ?? ""] ?? 9) ||
      templateRank(a.name) - templateRank(b.name) ||
      a.name.localeCompare(b.name),
  );
  return [...items.filter((item) => item.type !== "template"), ...templates];
}

/** The catalog is a separate chunk so the console bundle stays small. */
export async function loadPluginCatalog(): Promise<CatalogEntry[]> {
  const data = (await import("./data/plugin-catalog.json")).default;
  return withBuiltinTemplates(data as CatalogEntry[]);
}
