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

// Entries this Go build implements. Keep in step with the channel factory
// (internal/channelconfig) and the mail/template services.
export const builtinPlugins: Readonly<Record<string, BuiltinPlugin>> = {
  "payment:alipay": {
    note: "Go 版内置实现，按通道配置提供收款、退款、转账及分账等能力。",
  },
  "payment:wxpay": {
    note: "Go 版内置实现，同一插件可配置微信支付 V2 或 V3 接口。",
  },
  "profitsharing:alipay": { note: "Go 版内置实现，在“分账管理”中使用。" },
  "profitsharing:wxpay": { note: "Go 版内置实现，在“分账管理”中使用。" },
  "mail:smtp": { note: "Go 版内置实现，在“系统设置 › 邮箱配置”中使用。" },
  "template:home:default": { note: "Go 版内置模板，在“模板配置”中使用。" },
  "template:pay:default": { note: "Go 版内置模板，在“模板配置”中使用。" },
  "template:qrpay:default": { note: "Go 版内置模板，在“模板配置”中使用。" },
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

/** The catalog is a separate chunk so the console bundle stays small. */
export async function loadPluginCatalog(): Promise<CatalogEntry[]> {
  const data = (await import("./data/plugin-catalog.json")).default;
  return data as CatalogEntry[];
}
