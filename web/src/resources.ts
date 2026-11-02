import type { Area } from "./menu";
export interface ResourceEntry {
  mode: "create" | "edit";
  id?: string;
  returnPath: string;
}
export function resourceEntryFor(
  area: Area,
  path: string,
  id: unknown,
): ResourceEntry | undefined {
  if (!(
    (area === "merchant" && path === "/applyments/create") ||
    path === "/applyments/form"
  ))
    return;
  const returnPath = `/${area}/applyments/merchant`;
  if (path === "/applyments/create" || id === undefined)
    return { mode: "create", returnPath };
  return {
    mode: "edit",
    id:
      typeof id === "string" &&
      /^[1-9]\d{0,18}$/.test(id) &&
      BigInt(id) <= 9223372036854775807n
        ? id
        : "",
    returnPath,
  };
}
export type FieldType =
  | "string"
  | "text"
  | "password"
  | "email"
  | "url"
  | "integer"
  | "number"
  | "boolean"
  | "select"
  | "json"
  | "money"
  | "date"
  | "permission_tree";
export interface ResourceField {
  name: string;
  label: string;
  type: FieldType;
  required?: boolean;
  options?: ({ label: string; value: string | number } | string)[];
  readonly?: boolean;
  read_only?: boolean;
  write_only?: boolean;
  create_only?: boolean;
  description?: string;
  min?: number;
  max?: number;
  max_length?: number;
  pattern?: string;
  default?: unknown;
}
export interface ResourceFilterSpec {
  param: string;
  label: string;
  type?: "positive_id";
  options: { label: string; value: string }[];
}
export interface ResourceSchema {
  name?: string;
  title: string;
  description?: string;
  fields: ResourceField[];
  permissions: { create: boolean; update: boolean; delete: boolean };
  actions?: (string | { name: string; label: string })[];
  filters?: ResourceFilterSpec[];
  exportable?: boolean;
  batch_delete?: boolean;
  batch_status?: boolean;
}
export const resourcePages: Record<Area, Record<string, string>> = {
  admin: {
    "/system/order-subject-policies": "order-subject-policies",
    "/merchant/merchant-list": "merchants",
    "/merchant/group-list": "groups",
    "/merchant/domain": "domains",
    "/merchant/invite-code": "invitation-codes",
    "/system/anounce": "announcements",
    "/system/settings": "settings",
    "/system/admin-user": "admin-users",
    "/payapi/pay-channel": "channels",
    "/trade/blacklist": "blacklist",
    "/other/onecode": "collection-styles",
    "/other/onecode/edit": "collection-styles",
    "/payapi/pay-type": "pay-types",
    "/payapi/pay-roll": "pay-rolls",
    "/payapi/pay-weixin": "weixin-apps",
    "/applyments/channel": "onboarding-channels",
    "/applyments/merchant": "onboarding-applications",
    "/applyments/subchannel": "subchannels",
    "/applyments/mchrisk": "merchant-risk",
    "/applyments/form": "onboarding-applications",
    "/complain/list": "complaints",
    "/complain/task": "complaint-tasks",
    "/other/risk": "risk-records",
    "/profitsharing/internal": "profitsharing-rules",
    "/app/template": "template-configs",
    "/app/addon": "addon-configs",
  },
  merchant: {
    "/domain": "domains",
    "/settle/settings": "settlement-settings",
    "/onecode": "collection-codes",
    "/subchannel": "subchannels",
    "/complain": "complaints",
    "/applyments/merchant": "onboarding-applications",
    "/applyments/create": "onboarding-applications",
    "/applyments/form": "onboarding-applications",
    "/applyments/mchrisk": "merchant-risk",
    "/account/groupbuy": "groups",
  },
};
export const resourceFor = (area: Area, path: string) =>
  resourcePages[area][path];
export const readonly = (f: ResourceField, editing = false) =>
  !!(f.readonly || f.read_only || (editing && f.create_only));
const empty = (v: unknown) => v === undefined || v === null || v === "";
export function validateFields(
  schema: ResourceSchema,
  values: Record<string, unknown>,
  editing = false,
): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const f of schema.fields) {
    if (readonly(f, editing)) continue;
    if (f.type === "permission_tree") continue;
    const v = values[f.name];
    if (empty(v)) {
      if (f.required && !(editing && (f.write_only || f.type === "password")))
        errors[f.name] = `请填写${f.label}`;
      continue;
    }
    const s = String(v);
    if (f.max_length && s.length > f.max_length)
      errors[f.name] = `${f.label}最多 ${f.max_length} 个字符`;
    if (
      f.type === "integer" &&
      (!/^-?\d+$/.test(s) || !Number.isSafeInteger(Number(s)))
    )
      errors[f.name] = "请输入有效整数";
    if (
      (f.type === "integer" || f.type === "number") &&
      (Number.isNaN(Number(s)) ||
        (f.min !== undefined && Number(s) < f.min) ||
        (f.max !== undefined && Number(s) > f.max))
    )
      errors[f.name] = "数值超出允许范围";
    if (f.type === "money" && !/^(0|[1-9]\d*)(\.\d{1,2})?$/.test(s))
      errors[f.name] = "请输入最多两位小数的金额";
    if (f.type === "json")
      try {
        JSON.parse(s);
      } catch {
        errors[f.name] = "请输入有效 JSON";
      }
    if (f.type === "email" && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s))
      errors[f.name] = "请输入有效邮箱";
    if (f.type === "url")
      try {
        const u = new URL(s);
        if (!["https:", "http:"].includes(u.protocol))
          errors[f.name] = "仅允许 HTTP 或 HTTPS 地址";
      } catch {
        errors[f.name] = "请输入有效地址";
      }
    if (
      f.options?.length &&
      !f.options.some((o) => String(typeof o === "string" ? o : o.value) === s)
    )
      errors[f.name] = "请选择有效选项";
    if (f.pattern)
      try {
        if (!new RegExp(f.pattern).test(s))
          errors[f.name] = `${f.label}格式不正确`;
      } catch {
        errors[f.name] = "字段规则错误，请联系管理员";
      }
  }
  return errors;
}
export function buildPayload(
  schema: ResourceSchema,
  values: Record<string, unknown>,
  editing = false,
): Record<string, unknown> {
  const payload: Record<string, unknown> = {};
  for (const f of schema.fields) {
    const v = values[f.name];
    if (
      readonly(f, editing) ||
      (empty(v) && editing && (f.write_only || f.type === "password")) ||
      v === undefined ||
      v === null ||
      (v === "" && ["integer", "number", "json", "money"].includes(f.type))
    )
      continue;
    payload[f.name] =
      f.type === "json"
        ? JSON.parse(String(v))
        : f.type === "permission_tree"
          ? values.admin_class === "super"
            ? []
            : (Array.isArray(v) ? v : []).map(String)
          : ["integer", "number"].includes(f.type)
            ? Number(v)
            : f.type === "boolean"
              ? Boolean(v)
              : v;
  }
  return payload;
}
export function formValues(
  schema: ResourceSchema,
  record: Record<string, unknown> = {},
): Record<string, unknown> {
  const editing = Object.keys(record).length > 0;
  return Object.fromEntries(
    schema.fields
      .filter((f) => !readonly(f, editing))
      .map((f) => {
        // A stored write-only value is never shown; a new record starts from
        // the field's default (for example 添加商户 手续费扣除模式).
        const value =
          f.type === "password" || (f.write_only && editing)
            ? ""
            : (record[f.name] ??
              f.default ??
              (f.type === "boolean"
                ? false
                : f.type === "permission_tree"
                  ? []
                  : ""));
        return [
          f.name,
          f.type === "json" && typeof value === "object"
            ? JSON.stringify(value, null, 2)
            : value,
        ];
      }),
  );
}
