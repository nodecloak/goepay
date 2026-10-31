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
export type TemplateSource = "builtin" | "upload";
export interface TemplateListItem {
  name: TemplateName;
  title: string;
  description: string;
  source: TemplateSource;
  // Upload items only.
  picture_url?: string;
  upload_version?: number;
  updated_at?: string;
  updated_by?: string;
  files?: number;
  size?: number;
}
export interface TemplateList {
  type: TemplateKind;
  revision: string;
  selected: string;
  items: TemplateListItem[];
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
  // the two checkout card layouts, and pay also an anime one. Administrators
  // add their own styles by uploading template packages (source "upload").
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
  ],
  cardNames = ["default", "compact"],
  payNames = [...cardNames, "anime"],
  assetID = /^[0-9a-f]{64}$/,
  // An uploaded template name: the server enforces the same rule.
  uploadName = /^[a-z][a-z0-9-]{0,30}[a-z0-9]$/;
function namesFor(kind: string): string[] {
  return kind === "home" ? homeNames : kind === "pay" ? payNames : cardNames;
}
function builtinName(kind: string, name: string): boolean {
  return namesFor(kind).includes(name);
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
// A config/body name is either a built-in name of the kind or a valid upload
// name (the server rejects an unknown upload, so the shape check is enough).
function configName(kind: string, name: string): boolean {
  return (
    builtinName(kind, name) ||
    (uploadName.test(name) && !isReservedName(name))
  );
}
export function templateConfigBody(
  view: TemplateConfigView,
  draft: Record<string, unknown>,
): Record<string, unknown> {
  if (
    !kinds.includes(view.type) ||
    !configName(view.type, view.name) ||
    !revision(view.revision)
  )
    throw new Error("请重新读取原模板版本");
  return { expected_revision: view.revision, config: config(view.type, draft) };
}
export function readTemplateConfig(raw: unknown): TemplateConfigView {
  const v = object(raw);
  if (
    !kinds.includes(String(v.type)) ||
    !configName(String(v.type), String(v.name)) ||
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
// Names that an uploaded template may never use: every built-in name of any
// kind, plus the reserved words the server keeps for its own routes. Mirrors
// internal/pagetemplate reserved-name rules so the console never shows an
// upload under a name the server would refuse.
const reservedWords = new Set([
  "default",
  "compact",
  "anime",
  "minimal",
  "sakura",
  "starry",
  "manga",
  "mecha",
  "candy",
  "custom",
  "upload",
  "uploads",
  "sample",
  "selection",
  "images",
  "files",
  "preview",
  "versions",
]);
function isReservedName(name: string): boolean {
  return reservedWords.has(name) || /^index[0-9]*$/.test(name);
}
function count(value: unknown): number {
  if (!Number.isSafeInteger(value) || Number(value) < 0)
    throw new Error("模板响应格式无效，请重新读取");
  return value as number;
}
function positive(value: unknown): number {
  const n = count(value);
  if (n < 1) throw new Error("模板响应格式无效，请重新读取");
  return n;
}
function text(value: unknown, field: string): string {
  if (typeof value !== "string") throw new Error(`${field}格式无效`);
  return value;
}
function pictureURL(value: unknown): string {
  // The server leaves the field out when a package has no preview picture.
  if (value === "" || value === undefined) return "";
  if (
    typeof value !== "string" ||
    !/^\/template-files\/[0-9a-f]{32}\/[^?#]+$/.test(value)
  )
    throw new Error("模板预览图地址无效，请重新读取");
  return value;
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
  const kind = v.type as TemplateKind;
  const allowed = namesFor(kind);
  const seenBuiltin = new Set<string>();
  const seen = new Set<string>();
  const items: TemplateListItem[] = v.items.map((raw) => {
    const item = object(raw);
    const name = String(item.name);
    if (
      seen.has(name) ||
      typeof item.title !== "string" ||
      typeof item.description !== "string" ||
      (item.source !== "builtin" && item.source !== "upload")
    )
      throw new Error("模板目录包含未审核项");
    seen.add(name);
    if (item.source === "builtin") {
      if (!allowed.includes(name)) throw new Error("模板目录包含未审核项");
      seenBuiltin.add(name);
      return {
        name: name as TemplateName,
        title: item.title,
        description: item.description,
        source: "builtin",
      };
    }
    // An uploaded item: a valid, non-reserved upload name and the upload fields.
    if (!uploadName.test(name) || isReservedName(name))
      throw new Error("模板目录包含未审核项");
    return {
      name: name as TemplateName,
      title: item.title,
      description: item.description,
      source: "upload",
      picture_url: pictureURL(item.picture_url),
      upload_version: positive(item.upload_version),
      updated_at: text(item.updated_at, "时间"),
      updated_by: text(item.updated_by, "操作人"),
      files: count(item.files),
      size: count(item.size),
    };
  });
  // Every built-in of the kind must be present exactly once; uploads are free.
  if (seenBuiltin.size !== allowed.length)
    throw new Error("模板目录读取失败");
  return {
    type: kind,
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

// --- Uploaded page templates ---------------------------------------------
//
// The super administrator uploads a .zip template package for any of the three
// kinds. The server checks the package, keeps every version immutable and
// notifies the administrator out of band. The console calls `check` first,
// shows what the package becomes, then `uploads` with the list revision. All
// responses are read strictly: an unexpected shape is rejected rather than
// shown. The byte limit (at most 10 MiB) is enforced before any request.
export const uploadPackageLimit = 10 * 1024 * 1024;
export type TemplateNoticeState = "pending" | "accepted" | "unknown";
export interface TemplateUploadReport {
  warnings: string[];
  removed: Record<string, number>;
  ignored: string[];
}
export interface TemplateUploadCheck extends TemplateUploadReport {
  type: TemplateKind;
  name: string;
  title: string;
  exists: boolean;
  selected: boolean;
  next_version: number;
  identical: boolean;
  files: number;
  size: number;
}
export interface TemplateUploadResult extends TemplateUploadReport {
  type: TemplateKind;
  name: string;
  title: string;
  version: number;
  revision: string;
  created: boolean;
  selected: boolean;
  files: number;
  size: number;
}
export interface TemplateVersionRow {
  version: number;
  operation: "upload" | "restore" | "remove" | "select";
  restored_from: number;
  title: string;
  package_version: string;
  author: string;
  files: number;
  size: number;
  created_at: string;
  actor: string;
  notice: TemplateNoticeState;
}
export interface TemplateRestoreResult {
  version: number;
  revision: string;
}
export interface TemplateRemoveResult {
  revision: string;
}
function noticeState(value: unknown): TemplateNoticeState {
  if (value !== "pending" && value !== "accepted" && value !== "unknown")
    throw new Error("模板通知状态无效，请重新读取");
  return value;
}
function operation(value: unknown): TemplateVersionRow["operation"] {
  if (
    value !== "upload" &&
    value !== "restore" &&
    value !== "remove" &&
    value !== "select"
  )
    throw new Error("模板版本操作无效，请重新读取");
  return value;
}
function packageVersion(value: unknown): string {
  if (typeof value !== "string" || !/^[0-9A-Za-z.+-]{0,32}$/.test(value))
    throw new Error("模板包版本号无效，请重新读取");
  return value;
}
function report(v: Record<string, unknown>): TemplateUploadReport {
  if (
    !Array.isArray(v.warnings) ||
    v.warnings.some((w) => typeof w !== "string") ||
    !Array.isArray(v.ignored) ||
    v.ignored.some((w) => typeof w !== "string")
  )
    throw new Error("模板处理报告无效，请重新读取");
  const removedRaw = v.removed === undefined ? {} : object(v.removed);
  const removed: Record<string, number> = {};
  for (const [key, value] of Object.entries(removedRaw)) {
    if (!key) throw new Error("模板移除报告无效，请重新读取");
    removed[key] = count(value);
  }
  return {
    warnings: v.warnings as string[],
    removed,
    ignored: v.ignored as string[],
  };
}
// POST /<kind>/uploads/check.
export function readUploadCheck(
  kind: TemplateKind,
  raw: unknown,
): TemplateUploadCheck {
  const v = object(raw);
  if (v.type !== kind) throw new Error("模板类型不一致，请重新读取");
  const name = String(v.name);
  if (!uploadName.test(name) || isReservedName(name))
    throw new Error("模板名称无效，请重新读取");
  if (
    typeof v.exists !== "boolean" ||
    typeof v.selected !== "boolean" ||
    typeof v.identical !== "boolean"
  )
    throw new Error("模板检查结果无效，请重新读取");
  return {
    type: kind,
    name,
    title: text(v.title, "标题"),
    exists: v.exists,
    selected: v.selected,
    next_version: positive(v.next_version),
    identical: v.identical,
    files: count(v.files),
    size: count(v.size),
    ...report(v),
  };
}
// POST /<kind>/uploads (201).
export function readUploadResult(
  kind: TemplateKind,
  raw: unknown,
): TemplateUploadResult {
  const v = object(raw);
  if (v.type !== kind) throw new Error("模板类型不一致，请重新读取");
  const name = String(v.name);
  if (!uploadName.test(name) || isReservedName(name))
    throw new Error("模板名称无效，请重新读取");
  if (
    !revision(v.revision) ||
    typeof v.created !== "boolean" ||
    typeof v.selected !== "boolean"
  )
    throw new Error("模板上传结果无效，请重新读取");
  return {
    type: kind,
    name,
    title: text(v.title, "标题"),
    version: positive(v.version),
    revision: v.revision,
    created: v.created,
    selected: v.selected,
    files: count(v.files),
    size: count(v.size),
    ...report(v),
  };
}
// GET /<kind>/<name>/versions (newest 50).
export function readTemplateVersions(raw: unknown): TemplateVersionRow[] {
  const v = object(raw);
  if (!Array.isArray(v.items) || v.items.length > 50)
    throw new Error("模板版本列表读取失败");
  const seen = new Set<number>();
  return v.items.map((raw) => {
    const item = object(raw);
    const n = positive(item.version);
    if (seen.has(n)) throw new Error("模板版本列表包含无效项");
    seen.add(n);
    const restored = count(item.restored_from ?? 0);
    if (restored >= n) throw new Error("模板版本列表包含无效项");
    return {
      version: n,
      operation: operation(item.operation),
      restored_from: restored,
      title: text(item.title, "标题"),
      package_version: packageVersion(item.package_version),
      author: text(item.author, "作者"),
      files: count(item.files),
      size: count(item.size),
      created_at: text(item.created_at, "时间"),
      actor: text(item.actor, "操作人"),
      notice: noticeState(item.notice),
    };
  });
}
// POST /<kind>/<name>/versions/<n>/restore.
export function readRestoreResult(raw: unknown): TemplateRestoreResult {
  const v = object(raw);
  if (!revision(v.revision)) throw new Error("模板恢复结果无效，请重新读取");
  return { version: positive(v.version), revision: v.revision };
}
// POST /<kind>/<name>/remove.
export function readRemoveResult(raw: unknown): TemplateRemoveResult {
  const v = object(raw);
  if (!revision(v.revision)) throw new Error("模板删除结果无效，请重新读取");
  return { revision: v.revision };
}
function assertUploadTarget(kind: TemplateKind, name: string): void {
  if (!kinds.includes(kind)) throw new Error("模板类型无效");
  if (!uploadName.test(name) || isReservedName(name))
    throw new Error("上传模板名称无效");
}
// The body for restore and remove: the list revision, checked as the server
// expects it (a decimal string).
export function templateRevisionBody(revisionValue: string): {
  expected_revision: string;
} {
  if (!revision(revisionValue)) throw new Error("请重新读取模板版本");
  return { expected_revision: revisionValue };
}
// --- Upload / sample multipart POST --------------------------------------
async function postTemplatePackage<T>(
  client: ApiClient,
  path: string,
  fields: Record<string, string>,
  file: File,
  read: (body: unknown) => T,
  signal?: AbortSignal,
): Promise<T> {
  if (client.area !== "admin" || !client.csrfToken)
    throw new Error("需要本人管理员登录");
  if (file.size < 1 || file.size > uploadPackageLimit)
    throw new Error("模板包大小须为1字节至10MiB");
  const form = new FormData();
  // expected_revision (and any other fields) first, then the file, as the
  // server reads the revision before the package.
  for (const [key, value] of Object.entries(fields)) form.append(key, value);
  form.append("file", file);
  const response = await fetch(`/console/v1/admin${path}`, {
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
  let body: { code?: number; message?: string; data?: unknown };
  try {
    body = await response.json();
  } catch {
    throw new Error("服务器返回格式错误，请稍后重试");
  }
  if (!response.ok || body.code !== 0)
    throw new TemplateUploadError(
      body.message || "模板包上传失败",
      response.status,
    );
  return read(body.data);
}
// A 409 on upload/restore/remove means the list revision moved; the console
// tells the user to reload rather than resending. A 422 carries a Chinese
// reason that is shown verbatim.
export class TemplateUploadError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export function checkTemplatePackage(
  client: ApiClient,
  kind: TemplateKind,
  file: File,
  signal?: AbortSignal,
): Promise<TemplateUploadCheck> {
  if (!kinds.includes(kind)) throw new Error("模板类型无效");
  return postTemplatePackage(
    client,
    `/templates/${kind}/uploads/check`,
    {},
    file,
    (body) => readUploadCheck(kind, body),
    signal,
  );
}
export function uploadTemplatePackage(
  client: ApiClient,
  kind: TemplateKind,
  expectedRevision: string,
  file: File,
  signal?: AbortSignal,
): Promise<TemplateUploadResult> {
  if (!kinds.includes(kind)) throw new Error("模板类型无效");
  if (!revision(expectedRevision)) throw new Error("请重新读取模板版本");
  return postTemplatePackage(
    client,
    `/templates/${kind}/uploads`,
    { expected_revision: expectedRevision },
    file,
    (body) => readUploadResult(kind, body),
    signal,
  );
}
// --- URL builders --------------------------------------------------------
// The sample package download.
export function templateSampleURL(kind: TemplateKind): string {
  if (!kinds.includes(kind)) throw new Error("模板类型无效");
  return `/console/v1/admin/templates/${kind}/sample`;
}
// A version's package download.
export function templateVersionPackageURL(
  kind: TemplateKind,
  name: string,
  version: number,
): string {
  assertUploadTarget(kind, name);
  if (!Number.isSafeInteger(version) || version < 1)
    throw new Error("模板版本号无效");
  return `/console/v1/admin/templates/${kind}/${name}/versions/${version}/package`;
}
// A version preview (sandboxed in an iframe by the caller).
export function templateVersionPreviewURL(
  kind: TemplateKind,
  name: string,
  version: number,
): string {
  assertUploadTarget(kind, name);
  if (!Number.isSafeInteger(version) || version < 1)
    throw new Error("模板版本号无效");
  return `/console/v1/admin/templates/${kind}/${name}/versions/${version}/preview`;
}
