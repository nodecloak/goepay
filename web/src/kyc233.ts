import { ApiClient, ApiError } from "./api";

export const certificationModes233 = [
  { value: "0", label: "关闭" },
  { value: "1", label: "支付宝身份验证" },
  { value: "3", label: "支付宝实名信息验证" },
  { value: "5", label: "阿里云金融级实人认证" },
  { value: "4", label: "微信扫码实名认证" },
  { value: "2", label: "手机号三要素实名认证" },
];
export const certificationCredentials233 = [
  "cert_appcode",
  "cert_appcode2",
  "cert_qcloudid",
  "cert_qcloudkey",
  "cert_qcloudruleid",
  "cert_aliyunid",
  "cert_aliyunkey",
  "cert_aliyunsceneid",
  "ocr_aliyunid",
  "ocr_aliyunkey",
  "ocr_baiduid",
  "ocr_baidukey",
] as const;
export type CredentialKey233 = (typeof certificationCredentials233)[number];
export interface CredentialView233 {
  present: boolean;
  masked: string;
}
export interface CredentialEdit233 {
  action: "keep" | "replace" | "clear";
  value: string;
}
export interface CertificationConfig233 {
  revision: string;
  cert_open: number;
  cert_channel: string;
  cert_corpopen: boolean;
  cert_force: boolean;
  cert_money: string;
  ocr_type: string;
  credentials: Record<CredentialKey233, CredentialView233>;
  channels: { id: string; name: string }[];
}
export interface CertificationInfo233 {
  revision: string;
  ocr_config_revision: string;
  configuration_changed: boolean;
  auth_code_used: boolean;
  auth_code_ready: boolean;
  query_after: string;
  cert: number;
  certtype: number;
  certcardtype: number;
  certmethod_text: string;
  certtype_text: string;
  certname: string;
  certno: string;
  certcorpname: string;
  certcorpno: string;
  certtime: string;
  cert_open: number;
  cert_corpopen: boolean;
  cert_money: string;
  phone_masked: string;
  ocr_type: string;
  attempt_id: string;
  status: string;
  scan: string;
  url: string;
  expires_at: string;
}
const decimal = /^(0|[1-9]\d{0,8})(?:\.\d{1,2})?$/;
const revision = /^(0|[1-9]\d{0,18})$/;
function record(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}
const invalid = () => Error("服务端响应不完整，请刷新页面后核对。 ");
export function parseConfig233(value: unknown): CertificationConfig233 {
  if (
    !record(value) ||
    !revision.test(String(value.revision)) ||
    typeof value.revision !== "string" ||
    ![0, 1, 2, 3, 4, 5].includes(Number(value.cert_open)) ||
    !Number.isInteger(value.cert_open) ||
    typeof value.cert_channel !== "string" ||
    !/^(|0|[1-9]\d{0,18})$/.test(value.cert_channel) ||
    typeof value.cert_corpopen !== "boolean" ||
    typeof value.cert_force !== "boolean" ||
    typeof value.cert_money !== "string" ||
    !decimal.test(value.cert_money) ||
    !["", "aliyun", "baidu"].includes(String(value.ocr_type)) ||
    typeof value.ocr_type !== "string" ||
    !record(value.credentials) ||
    !Array.isArray(value.channels)
  )
    throw invalid();
  const credentials = {} as CertificationConfig233["credentials"];
  for (const key of certificationCredentials233) {
    const entry = value.credentials[key];
    if (
      !record(entry) ||
      typeof entry.present !== "boolean" ||
      typeof entry.masked !== "string" ||
      entry.masked.length > 128
    )
      throw invalid();
    credentials[key] = { present: entry.present, masked: entry.masked };
  }
  const channels = value.channels.map((entry) => {
    if (
      !record(entry) ||
      typeof entry.id !== "string" ||
      !/^[1-9]\d{0,18}$/.test(entry.id) ||
      typeof entry.name !== "string" ||
      entry.name.length > 256
    )
      throw invalid();
    return { id: entry.id, name: entry.name };
  });
  return {
    revision: value.revision,
    cert_open: value.cert_open as number,
    cert_channel: value.cert_channel,
    cert_corpopen: value.cert_corpopen,
    cert_force: value.cert_force,
    cert_money: value.cert_money,
    ocr_type: value.ocr_type,
    credentials,
    channels,
  };
}
const statuses = [
  "none",
  "ready",
  "initializing",
  "pending",
  "unknown",
  "rejected",
  "passed",
  "verified",
  "expired",
  "cancelled",
];
export function parseInfo233(value: unknown): CertificationInfo233 {
  if (
    !record(value) ||
    typeof value.revision !== "string" ||
    !revision.test(value.revision) ||
    typeof value.ocr_config_revision !== "string" ||
    !revision.test(value.ocr_config_revision) ||
    typeof value.configuration_changed !== "boolean" ||
    typeof value.auth_code_used !== "boolean" ||
    typeof value.auth_code_ready !== "boolean" ||
    typeof value.query_after !== "string" ||
    (value.query_after !== "" &&
      !Number.isFinite(Date.parse(value.query_after))) ||
    ![0, 1].includes(Number(value.cert)) ||
    !Number.isInteger(value.cert) ||
    ![0, 1].includes(Number(value.certtype)) ||
    !Number.isInteger(value.certtype) ||
    !Number.isInteger(value.certcardtype) ||
    Number(value.certcardtype) < 0 ||
    Number(value.certcardtype) > 5 ||
    ![0, 1, 2, 3, 4, 5].includes(Number(value.cert_open)) ||
    !Number.isInteger(value.cert_open) ||
    typeof value.cert_corpopen !== "boolean" ||
    typeof value.cert_money !== "string" ||
    !decimal.test(value.cert_money) ||
    !statuses.includes(String(value.status))
  )
    throw invalid();
  const strings = [
    "certmethod_text",
    "certtype_text",
    "certname",
    "certno",
    "certcorpname",
    "certcorpno",
    "certtime",
    "phone_masked",
    "ocr_type",
    "attempt_id",
    "scan",
    "url",
    "expires_at",
  ];
  if (
    strings.some(
      (key) =>
        typeof value[key] !== "string" ||
        String(value[key]).length > (key === "url" ? 16384 : 512),
    ) ||
    !["", "aliyun", "baidu"].includes(String(value.ocr_type)) ||
    !["", "alipay", "wechat", "phone"].includes(String(value.scan))
  )
    throw invalid();
  if (value.url && !safeCertificationURL233(String(value.url))) throw invalid();
  return Object.fromEntries(
    [
      ...strings,
      "cert",
      "certtype",
      "certcardtype",
      "cert_open",
      "cert_corpopen",
      "cert_money",
      "status",
      "revision",
      "ocr_config_revision",
      "configuration_changed",
      "auth_code_used",
      "auth_code_ready",
      "query_after",
    ].map((key) => [key, value[key]]),
  ) as unknown as CertificationInfo233;
}
export function emptyCredentialEdits233(): Record<
  CredentialKey233,
  CredentialEdit233
> {
  return Object.fromEntries(
    certificationCredentials233.map((key) => [
      key,
      { action: "keep", value: "" },
    ]),
  ) as Record<CredentialKey233, CredentialEdit233>;
}
export function safeCertificationURL233(value: string): boolean {
  try {
    const u = new URL(value);
    if (
      u.protocol !== "https:" ||
      u.username ||
      u.password ||
      u.port ||
      /[\r\n\\]/.test(value) ||
      /%2f|%5c/i.test(u.pathname)
    )
      return false;
    // Fixed provider hosts only. Never render server-returned javascript/data URLs.
    if (u.hostname === "open.weixin.qq.com")
      return (
        u.pathname === "/connect/oauth2/authorize" &&
        ["", "#wechat_redirect"].includes(u.hash)
      );
    if (u.hash) return false;
    if (u.hostname === "faceid.qq.com") return u.pathname === "/";
    if (u.hostname === "openauth.alipay.com")
      return u.pathname === "/oauth2/publicAppAuthorize.htm";
    return (
      ["openapi.alipay.com", "picker.antcloudauth.aliyuncs.com"].includes(
        u.hostname,
      ) && u.pathname === "/gateway.do"
    );
  } catch {
    return false;
  }
}
export const ocrDocuments233 = [
  { value: "idcard", label: "身份证人像面" },
  { value: "idcard_back", label: "身份证国徽面" },
  { value: "passport", label: "护照" },
  { value: "bank_card", label: "银行卡" },
  { value: "business_license", label: "营业执照" },
  { value: "bank_account_license", label: "开户许可证" },
];
export const ocrLabels233: Record<string, string> = {
  id_no: "身份证号码",
  name: "姓名/名称",
  address: "地址",
  sex: "性别",
  ethnicity: "民族",
  birth_date: "出生日期",
  issue_authority: "签发机关",
  period_begin: "有效期开始",
  period_end: "有效期结束",
  passport_no: "护照号码",
  passport_type: "护照类型",
  last_name: "姓氏",
  first_name: "名字",
  country: "国籍",
  bank_name: "银行名称",
  card_type: "银行卡类型",
  card_no: "银行卡号",
  license_no: "统一社会信用代码",
  reg_date: "成立日期",
  legal_name: "法人姓名",
  type: "主体类型",
  registered_capital: "注册资本",
  business_scope: "经营范围",
  bank_account: "银行账户",
  approval_no: "核准号",
  permit_no: "许可证编号",
};
export async function recognizeOCR233(
  client: ApiClient,
  provider: string,
  configRevision: string,
  type: string,
  file: File,
  operation: string,
  signal?: AbortSignal,
): Promise<Record<string, string>> {
  if (
    !["admin", "merchant"].includes(client.area) ||
    !["aliyun", "baidu"].includes(provider) ||
    !revision.test(configRevision) ||
    !client.csrfToken ||
    !ocrDocuments233.some((d) => d.value === type) ||
    !/^[a-f0-9]{32}$/.test(operation) ||
    file.size < 1 ||
    file.size > (provider === "baidu" ? 2 : 4) * 1024 * 1024 ||
    !["image/png", "image/jpeg"].includes(file.type)
  ) {
    throw Error(
      `请选择不超过${provider === "baidu" ? 2 : 4}MiB的PNG或JPEG证照图片，并确认本人会话有效。`,
    );
  }
  const area = client.area,
    csrf = client.csrfToken;
  const form = new FormData();
  form.set("document_type", type);
  form.set("image", file);
  form.set("operation_id", operation);
  form.set("expected_provider", provider);
  form.set("expected_revision", configRevision);
  const response = await fetch(`/console/v1/${area}/account/ocr`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "X-CSRF-Token": csrf, Accept: "application/json" },
    body: form,
    signal,
  });
  if (area !== client.area || csrf !== client.csrfToken || signal?.aborted)
    throw Error("会话已变化，请刷新页面后核对。");
  if (response.status === 401) {
    client.csrfToken = "";
    client.onUnauthorized?.();
  }
  const text = await response.text();
  if (text.length > 65536) throw invalid();
  let out: unknown;
  try {
    out = JSON.parse(text);
  } catch {
    throw invalid();
  }
  if (!record(out) || typeof out.code !== "number") throw invalid();
  if (!response.ok || out.code !== 0)
    throw new ApiError(
      typeof out.message === "string" ? out.message : "证照识别未完成",
      response.status,
      out.code,
    );
  if (!record(out.data) || !record(out.data.fields)) throw invalid();
  const fields: Record<string, string> = {};
  for (const [key, value] of Object.entries(out.data.fields)) {
    if (
      !Object.hasOwn(ocrLabels233, key) ||
      typeof value !== "string" ||
      value.length > 4096
    )
      throw invalid();
    fields[key] = value;
  }
  if (!Object.keys(fields).length)
    throw Error("未识别到证照字段，请检查文档类型与图片清晰度。");
  return fields;
}
