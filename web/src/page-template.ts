import type { ApiClient } from "./api";
import builtinTemplateData from "./data/builtin-templates.json";
export type TemplateKind = "home" | "pay" | "qrpay";
export type TemplateName = string;
export interface TemplateField {
  name: string;
  label: string;
  type: "text" | "textarea" | "color" | "image" | "select" | "switch";
  required: boolean;
  max_length?: number;
  options?: { label: string; value: string }[];
}
export interface TemplateConfigView {
  type: TemplateKind;
  name: TemplateName;
  revision: string;
  config: Record<string, string | boolean>;
  fields: TemplateField[];
  fallback?: string;
}
export interface TemplateList {
  type: TemplateKind;
  revision: string;
  selected: string;
  items: { name: TemplateName; title: string; description: string }[];
}
export interface TemplateAsset {
  id: string;
  mime: "image/png" | "image/jpeg";
  width: number;
  height: number;
}
const kinds = ["home", "pay", "qrpay"],
  // The home kind ships the original template names (default, index1-10),
  // GOEPAY's single-card style and five anime (二次元) styles; pay/qrpay ship
  // the two checkout card layouts, and pay also an anime one. The console
  // only ever selects a built-in name.
  homeNames = [
    "default",
    "index1",
    "index2",
    "index3",
    "index4",
    "index5",
    "index6",
    "index7",
    "index8",
    "index9",
    "index10",
    "minimal",
    "sakura",
    "starry",
    "manga",
    "mecha",
    "candy",
    "custom",
  ],
  cardNames = ["default", "compact"],
  payNames = [...cardNames, "anime"],
  assetID = /^[0-9a-f]{64}$/;
function namesFor(kind: string): string[] {
  return kind === "home" ? homeNames : kind === "pay" ? payNames : cardNames;
}
/**
 * The preview picture of a built-in template, generated into
 * web/public/static/template-previews by web/scripts/template-previews.mjs.
 */
export function templatePreviewImage(kind: string, name: string): string {
  const image = `/static/template-previews/${encodeURIComponent(kind)}-${encodeURIComponent(name)}.jpg`;
  const version =
    kind === "home"
      ? builtinTemplateData.find((entry) => entry.type === kind && entry.name === name)
          ?.version
      : undefined;
  return version ? `${image}?v=${encodeURIComponent(version)}` : image;
}
function revision(v: unknown): v is string {
  return (
    typeof v === "string" &&
    /^(0|[1-9]\d*)$/.test(v) &&
    BigInt(v) <= 9223372036854775807n
  );
}
function object(raw: unknown): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw new Error("模板响应格式无效，请重新读取");
  return raw as Record<string, unknown>;
}
function config(
  kind: TemplateKind,
  raw: unknown,
): Record<string, string | boolean> {
  const value = object(raw),
    allowed = [
      "brand_name",
      "notice",
      "accent_color",
      "background_color",
      "logo_asset_id",
      "density",
      ...(kind === "pay" ? ["show_order_name"] : []),
    ];
  if (
    Object.keys(value).length !== allowed.length ||
    Object.keys(value).some((k) => !allowed.includes(k))
  )
    throw new Error("仅能保存已审核的模板字段");
  const out: Record<string, string | boolean> = {};
  for (const key of allowed) {
    const v = value[key];
    if (key === "show_order_name") {
      if (typeof v !== "boolean") throw new Error("显示订单名称必须为开关值");
      out[key] = v;
      continue;
    }
    if (
      typeof v !== "string" ||
      /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(v)
    )
      throw new Error("模板字段类型或字符无效");
    out[key] = v;
  }
  if (
    !String(out.brand_name).trim() ||
    [...String(out.brand_name)].length > 64 ||
    /[\r\n\t]/.test(String(out.brand_name)) ||
    [...String(out.notice)].length > 240 ||
    !/^#[0-9a-fA-F]{6}$/.test(String(out.accent_color)) ||
    !/^#[0-9a-fA-F]{6}$/.test(String(out.background_color)) ||
    !["comfortable", "compact"].includes(String(out.density)) ||
    (out.logo_asset_id !== "" && !assetID.test(String(out.logo_asset_id)))
  )
    throw new Error("请核对名称、说明、颜色、间距和本地图片ID");
  return out;
}
export function templateConfigBody(
  view: TemplateConfigView,
  draft: Record<string, unknown>,
): Record<string, unknown> {
  if (
    !kinds.includes(view.type) ||
    !namesFor(view.type).includes(view.name) ||
    !revision(view.revision)
  )
    throw new Error("请重新读取原模板版本");
  return { expected_revision: view.revision, config: config(view.type, draft) };
}
export function readTemplateConfig(raw: unknown): TemplateConfigView {
  const v = object(raw);
  if (
    !kinds.includes(String(v.type)) ||
    !namesFor(String(v.type)).includes(String(v.name)) ||
    !revision(v.revision) ||
    !Array.isArray(v.fields)
  )
    throw new Error("模板类型、名称或版本无效");
  const kind = v.type as TemplateKind;
  const expected: Record<string, string> = {
    brand_name: "text",
    notice: "textarea",
    accent_color: "color",
    background_color: "color",
    logo_asset_id: "image",
    density: "select",
    ...(kind === "pay" ? { show_order_name: "switch" } : {}),
  };
  const fields = v.fields.map((raw) => {
    const f = object(raw);
    if (
      typeof f.name !== "string" ||
      typeof f.label !== "string" ||
      f.type !== expected[f.name]
    )
      throw new Error("模板字段定义未通过校验");
    return {
      name: f.name,
      label: f.label,
      type: f.type,
      required: f.required === true,
      max_length: typeof f.max_length === "number" ? f.max_length : undefined,
      options:
        f.name === "density"
          ? [
              { label: "舒适", value: "comfortable" },
              { label: "紧凑", value: "compact" },
            ]
          : undefined,
    } as TemplateField;
  });
  return {
    type: kind,
    name: v.name as TemplateName,
    revision: v.revision,
    config: config(kind, v.config),
    fields,
    fallback: typeof v.fallback === "string" ? v.fallback : undefined,
  };
}
export function readTemplateList(raw: unknown): TemplateList {
  const v = object(raw);
  if (
    !kinds.includes(String(v.type)) ||
    !revision(v.revision) ||
    typeof v.selected !== "string" ||
    !Array.isArray(v.items)
  )
    throw new Error("模板目录读取失败");
  const allowed = namesFor(String(v.type));
  if (v.items.length !== allowed.length) throw new Error("模板目录读取失败");
  const seen = new Set<string>();
  const items = v.items.map((raw) => {
    const item = object(raw);
    if (
      !allowed.includes(String(item.name)) ||
      seen.has(String(item.name)) ||
      typeof item.title !== "string" ||
      typeof item.description !== "string"
    )
      throw new Error("模板目录包含未审核项");
    seen.add(String(item.name));
    return {
      name: item.name as TemplateName,
      title: item.title,
      description: item.description,
    };
  });
  return {
    type: v.type as TemplateKind,
    revision: v.revision,
    selected: v.selected,
    items,
  };
}
export async function uploadTemplateImage(
  client: ApiClient,
  file: File,
  signal?: AbortSignal,
): Promise<TemplateAsset> {
  if (client.area !== "admin" || !client.csrfToken)
    throw new Error("需要本人管理员登录");
  if (file.size < 1 || file.size > 512 * 1024)
    throw new Error("图片大小须为1–512KiB");
  const form = new FormData();
  form.append("file", file);
  const response = await fetch("/console/v1/admin/templates/images", {
    method: "POST",
    headers: { "X-CSRF-Token": client.csrfToken, Accept: "application/json" },
    body: form,
    credentials: "same-origin",
    signal,
  });
  if (response.status === 401) {
    client.csrfToken = "";
    client.onUnauthorized?.();
  }
  const body = await response.json();
  if (!response.ok || body.code !== 0)
    throw new Error(body.message || "图片上传失败");
  const a = object(body.data);
  if (
    !assetID.test(String(a.id)) ||
    !["image/png", "image/jpeg"].includes(String(a.mime)) ||
    !Number.isSafeInteger(a.width) ||
    !Number.isSafeInteger(a.height) ||
    Number(a.width) < 1 ||
    Number(a.height) < 1 ||
    Number(a.width) > 2048 ||
    Number(a.height) > 2048 ||
    Number(a.width) * Number(a.height) > 1048576
  )
    throw new Error("图片响应未通过校验");
  return a as unknown as TemplateAsset;
}

// --- Custom HTML home template -------------------------------------------
//
// The super administrator pastes raw HTML + CSS for the 自定义 HTML 首页模板
// ("custom" home name). Every save/restore appends an immutable version and
// notifies the administrator out of band. The server sanitizes the HTML at
// render time; the console only enforces the source byte limits and reads the
// typed responses strictly, rejecting any unexpected shape.
export const customHtmlLimit = 65536,
  customCssLimit = 32768;
export type CustomNoticeState = "pending" | "accepted" | "unknown";
export interface CustomHtmlVersion {
  version: number;
  revision: string;
  html: string;
  css: string;
  frame: boolean;
  created_at: string;
  actor: string;
  restored_from: number;
}
export interface CustomHtmlSample {
  html: string;
  css: string;
  frame: boolean;
}
export interface CustomVersionRow {
  version: number;
  created_at: string;
  actor: string;
  html_bytes: number;
  css_bytes: number;
  frame: boolean;
  restored_from: number;
  notice: CustomNoticeState;
}
export interface CustomSaveResult {
  version: number;
  revision: string;
  removed: Record<string, number>;
}
const utf8Bytes = (value: string): number =>
  typeof TextEncoder !== "undefined"
    ? new TextEncoder().encode(value).length
    : // Fallback for environments without TextEncoder (unused in the app).
      unescape(encodeURIComponent(value)).length;
export function customHtmlByteLength(value: string): number {
  return utf8Bytes(value);
}
function text(value: unknown, field: string): string {
  if (typeof value !== "string") throw new Error(`${field}格式无效`);
  return value;
}
function count(value: unknown): number {
  if (!Number.isSafeInteger(value) || Number(value) < 0)
    throw new Error("自定义模板响应格式无效，请重新读取");
  return value as number;
}
function version(value: unknown): number {
  if (!Number.isSafeInteger(value) || Number(value) < 0)
    throw new Error("自定义模板版本号无效，请重新读取");
  return value as number;
}
function frame(value: unknown): boolean {
  if (typeof value !== "boolean")
    throw new Error("“保留系统导航和页脚”必须为开关值");
  return value;
}
function noticeState(value: unknown): CustomNoticeState {
  if (value !== "pending" && value !== "accepted" && value !== "unknown")
    throw new Error("自定义模板通知状态无效，请重新读取");
  return value;
}
/** GET /home/custom/html and GET /home/custom/versions/<n>. */
export function readCustomHtml(raw: unknown): CustomHtmlVersion {
  const v = object(raw);
  const n = version(v.version);
  if (n === 0)
    return {
      version: 0,
      revision: revision(v.revision) ? (v.revision as string) : "0",
      html: typeof v.html === "string" ? v.html : "",
      css: typeof v.css === "string" ? v.css : "",
      frame: v.frame === undefined ? true : frame(v.frame),
      created_at: typeof v.created_at === "string" ? v.created_at : "",
      actor: typeof v.actor === "string" ? v.actor : "",
      restored_from: 0,
    };
  if (!revision(v.revision)) throw new Error("自定义模板修订号无效，请重新读取");
  const restored = count(v.restored_from ?? 0);
  if (restored >= n)
    throw new Error("自定义模板来源版本无效，请重新读取");
  return {
    version: n,
    revision: v.revision as string,
    html: text(v.html, "HTML"),
    css: text(v.css, "CSS"),
    frame: frame(v.frame),
    created_at: text(v.created_at, "时间"),
    actor: text(v.actor, "操作人"),
    restored_from: restored,
  };
}
/** GET /home/custom/sample. */
export function readCustomSample(raw: unknown): CustomHtmlSample {
  const v = object(raw);
  return {
    html: text(v.html, "HTML"),
    css: text(v.css, "CSS"),
    frame: frame(v.frame),
  };
}
/** GET /home/custom/versions (newest first, at most 50). */
export function readCustomVersions(raw: unknown): CustomVersionRow[] {
  const v = object(raw);
  if (!Array.isArray(v.items)) throw new Error("自定义模板版本列表读取失败");
  if (v.items.length > 50) throw new Error("自定义模板版本列表读取失败");
  const seen = new Set<number>();
  return v.items.map((raw) => {
    const item = object(raw);
    const n = version(item.version);
    if (n < 1 || seen.has(n)) throw new Error("自定义模板版本列表包含无效项");
    seen.add(n);
    const restored = count(item.restored_from ?? 0);
    if (restored >= n) throw new Error("自定义模板版本列表包含无效项");
    return {
      version: n,
      created_at: text(item.created_at, "时间"),
      actor: text(item.actor, "操作人"),
      html_bytes: count(item.html_bytes),
      css_bytes: count(item.css_bytes),
      frame: frame(item.frame),
      restored_from: restored,
      notice: noticeState(item.notice),
    };
  });
}
/** PUT /home/custom/html and POST /home/custom/versions/<n>/restore. */
export function readCustomSaveResult(raw: unknown): CustomSaveResult {
  const v = object(raw);
  const n = version(v.version);
  if (n < 1 || !revision(v.revision))
    throw new Error("自定义模板保存结果无效，请重新读取");
  const removedRaw = v.removed === undefined ? {} : object(v.removed);
  const removed: Record<string, number> = {};
  for (const [key, value] of Object.entries(removedRaw)) {
    if (typeof key !== "string" || !key)
      throw new Error("自定义模板移除报告无效，请重新读取");
    removed[key] = count(value);
  }
  return { version: n, revision: v.revision as string, removed };
}
/**
 * PUT /home/custom/html body. Enforces the UTF-8 byte limits (the server also
 * enforces them, with a Chinese reason, but the editor catches them first).
 */
export function customHtmlBody(
  revisionValue: string,
  html: string,
  css: string,
  frameValue: boolean,
): { expected_revision: string; html: string; css: string; frame: boolean } {
  if (!revision(revisionValue)) throw new Error("请重新读取自定义模板版本");
  if (typeof html !== "string" || typeof css !== "string")
    throw new Error("HTML 和 CSS 必须为文本");
  if (typeof frameValue !== "boolean")
    throw new Error("“保留系统导航和页脚”必须为开关值");
  if (customHtmlByteLength(html) > customHtmlLimit)
    throw new Error("HTML 超过 64 KiB");
  if (customHtmlByteLength(css) > customCssLimit)
    throw new Error("CSS 超过 32 KiB");
  return {
    expected_revision: revisionValue,
    html,
    css,
    frame: frameValue,
  };
}
