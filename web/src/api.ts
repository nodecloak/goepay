import type { Area } from "./menu";
import { isAdminFundsAction } from "./funds-authorization";
import { parseMerchantKeySession } from "./merchant-api-switches";
import { isPersonalMerchantID } from "./merchant-personal-settings";
export interface FundsActionRequest {
  target: string;
  body: Record<string, unknown>;
  signal?: AbortSignal;
  // Called synchronously only at original-business dispatch, never for status,
  // challenge or password verification. A storage failure must stop the POST.
  beforeDispatch?: () => void;
}
export type FundsActionResult = { status: number; data: unknown };
export interface User {
  id: string | number;
  name: string;
  role: string;
  admin_class?: "super" | "operator";
  // Per-menu grant set for a restricted operator (menu node ids). Absent for
  // super and for an unrestricted operator (both see every permitted menu).
  permissions?: string[];
}
export interface Session {
  user: User;
  csrf_token: string;
  delegated_by?: { id: string | number; name: string } | null;
  // Grantable per-menu permission catalog for the admin-user editor (admins
  // only). Each entry is { key, title, group }.
  admin_permission_catalog?: { key: string; title: string; group: string }[];
  // Merchant-center feature toggles (merchant sessions only). A key maps to a
  // menu node id whose entry is hidden when the feature is off. Supplied by the
  // server session endpoint; absent for admin sessions.
  merchant_features?: Record<string, boolean>;
}
const ownMerchantSessionErrors = new Set([
  "original merchant session required",
  "own merchant session required",
  "own authenticated session required",
  "own merchant session required for membership purchase",
]);
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: number,
    public retryAfterSeconds?: number,
    public responseDecoded = true,
    public requestNotRecorded?: {
      kind: string;
      merchant_id: string;
      external_no: string;
      reason: string;
    },
  ) {
    super(
      status === 403 && ownMerchantSessionErrors.has(message)
        ? "该功能需要商户本人登录，请使用商户账号登录后操作。"
        : message,
    );
  }
}
export class ApiClient {
  csrfToken = "";
  onUnauthorized?: () => void;
  beforeFundsAction?: (
    action: FundsActionRequest,
    send: () => Promise<FundsActionResult>,
  ) => Promise<FundsActionResult>;
  constructor(public area: Area) {}
  async request<T = unknown>(
    path: string,
    options: {
      method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
      body?: unknown;
      signal?: AbortSignal;
      paymentAuthorization?: string;
      fundsAuthorizationChecked?: boolean;
      beforeDispatch?: () => void;
    } = {},
  ): Promise<T> {
    return (await this.requestResult<T>(path, options)).data;
  }
  async requestResult<T = unknown>(
    path: string,
    options: {
      method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
      body?: unknown;
      signal?: AbortSignal;
      paymentAuthorization?: string;
      fundsAuthorizationChecked?: boolean;
      beforeDispatch?: () => void;
    } = {},
  ): Promise<{ status: number; data: T }> {
    const method = options.method || "GET";
    if (
      this.area === "admin" &&
      this.beforeFundsAction &&
      isAdminFundsAction(path, method) &&
      options.paymentAuthorization === undefined &&
      !options.fundsAuthorizationChecked
    ) {
      const result = await this.beforeFundsAction(
        {
          target: `/console/v1/admin${path}`,
          body: (options.body || {}) as Record<string, unknown>,
          signal: options.signal,
          beforeDispatch: options.beforeDispatch,
        },
        () =>
          this.requestResult(path, {
            ...options,
            fundsAuthorizationChecked: true,
          }),
      );
      return result as { status: number; data: T };
    }
    const headers: Record<string, string> = { Accept: "application/json" };
    if (options.paymentAuthorization !== undefined) {
      if (
        this.area !== "admin" ||
        method !== "POST" ||
        !options.paymentAuthorization ||
        /[\r\n]/.test(options.paymentAuthorization)
      )
        throw new Error("资金授权仅能用于管理员的当次 POST 操作");
      headers["X-Payment-Authorization"] = options.paymentAuthorization;
    }
    if (method !== "GET") {
      if (
        !this.csrfToken &&
        path !== "/login" &&
        !(
          this.area === "merchant" &&
          path === "/key-login" &&
          method === "POST"
        )
      )
        throw new Error("会话已失效，请重新登录");
      headers["Content-Type"] = "application/json";
      if (this.csrfToken) headers["X-CSRF-Token"] = this.csrfToken;
    }
    const keyLogin = this.area === "merchant" && path === "/key-login";
    const requestArea = this.area;
    const requestCSRF = this.csrfToken;
    const requestBody =
      options.body === undefined ? undefined : JSON.stringify(options.body);
    if (options.signal?.aborted)
      throw (
        options.signal.reason || new DOMException("请求已取消", "AbortError")
      );
    options.beforeDispatch?.();
    const response = await fetch(`/console/v1/${this.area}${path}`, {
      method,
      credentials: "same-origin",
      headers,
      body: requestBody,
      signal: options.signal,
    });
    let payload: { code: number; message?: string; data: T };
    try {
      payload = await response.json();
    } catch {
      throw new ApiError(
        "服务器返回格式错误，请稍后重试",
        response.status,
        -1,
        undefined,
        false,
      );
    }
    if (keyLogin && requestCSRF !== this.csrfToken)
      throw new Error("登录上下文已变化，请重新检查登录状态");
    if (
      response.status === 401 &&
      requestArea === this.area &&
      requestCSRF === this.csrfToken &&
      !options.signal?.aborted
    ) {
      this.csrfToken = "";
      this.onUnauthorized?.();
    }
    if (!response.ok || payload.code !== 0)
      throw new ApiError(
        payload.message || "请求失败",
        response.status,
        payload.code,
        /^\d{1,5}$/.test(response.headers.get("Retry-After") || "")
          ? Number(response.headers.get("Retry-After"))
          : undefined,
        true,
        workflowRefusalProof212(
          response.status,
          path,
          options.body,
          payload.data,
        ),
      );
    return { status: response.status, data: payload.data };
  }
  async session() {
    const s = await this.request<Session>("/session");
    this.csrfToken = s.csrf_token;
    return s;
  }
  async downloadCSV(
    path: string,
    filename: string,
    options: { signal?: AbortSignal; encoding?: "utf8-bom" | "gbk" } = {},
  ): Promise<void> {
    const gbk = options.encoding === "gbk";
    if (
      gbk &&
      (this.area !== "admin" ||
        !/^\/settlement-batches\/[1-9]\d*\/export\?format=(alipay|mybank)$/.test(
          path,
        ))
    )
      throw new Error("GBK 仅用于原结算批次的支付宝及网商银行附件");
    const requestArea = this.area;
    const requestCSRF = this.csrfToken;
    const response = await fetch(`/console/v1/${this.area}${path}`, {
      method: "GET",
      credentials: "same-origin",
      headers: { Accept: "text/csv" },
      signal: options.signal,
    });
    if (
      response.status === 401 &&
      requestArea === this.area &&
      requestCSRF === this.csrfToken &&
      !options.signal?.aborted
    ) {
      this.csrfToken = "";
      this.onUnauthorized?.();
    }
    if (
      response.status !== 200 ||
      response.headers
        .get("Content-Type")
        ?.split(";")[0]
        ?.trim()
        .toLowerCase() !== "text/csv"
    ) {
      let message = "导出失败：服务端未返回 CSV 附件，请稍后重试",
        code = -1;
      try {
        const payload = await response.json();
        if (payload.message) message = String(payload.message);
        if (typeof payload.code === "number") code = payload.code;
      } catch {}
      throw new ApiError(message, response.status, code);
    }
    if (
      !/^\s*attachment(?:\s*;|$)/i.test(
        response.headers.get("Content-Disposition") || "",
      )
    )
      throw new ApiError(
        "导出响应缺少 CSV 附件标识，未保存文件",
        response.status,
        -1,
      );
    if (
      gbk &&
      !/;\s*charset\s*=\s*gbk\s*$/i.test(
        response.headers.get("Content-Type") || "",
      )
    )
      throw new Error("原结算批次附件编码不符，未保存文件");
    const maximum = 8 * 1024 * 1024;
    if (Number(response.headers.get("Content-Length")) > maximum) {
      await response.body?.cancel();
      throw new Error("导出超过 8 MiB，请缩小筛选范围");
    }
    const reader = response.body?.getReader();
    if (!reader) throw new Error("导出响应内容不可读取，未保存文件");
    const chunks: BlobPart[] = [];
    const prefix: number[] = [];
    let size = 0;
    try {
      while (true) {
        const result = await reader.read();
        if (result.done) break;
        size += result.value.byteLength;
        if (size > maximum) {
          await reader.cancel();
          throw new Error("导出超过 8 MiB，请缩小筛选范围");
        }
        for (const byte of result.value.subarray(0, 3 - prefix.length))
          prefix.push(byte);
        chunks.push(result.value.slice().buffer);
      }
    } finally {
      reader.releaseLock();
    }
    const bom = prefix[0] === 0xef && prefix[1] === 0xbb && prefix[2] === 0xbf;
    const blob = new Blob(chunks, {
      type: gbk ? "text/csv;charset=gbk" : "text/csv;charset=utf-8",
    });
    if (gbk && (bom || size === 0))
      throw new Error("原 GBK 附件内容不符，未保存文件");
    if (!gbk && !bom)
      throw new Error("导出响应缺少 UTF-8 CSV 标识，未保存文件");
    if (options.signal?.aborted)
      throw new DOMException("导出已取消", "AbortError");
    const url = URL.createObjectURL(blob),
      a = document.createElement("a");
    try {
      a.href = url;
      const safe =
        filename
          .replace(/[^a-zA-Z0-9._-]/g, "_")
          .replace(/^\.+/, "")
          .slice(0, 120) || "export.csv";
      a.download = safe.endsWith(".csv") ? safe : `${safe}.csv`;
      document.body.append(a);
      a.click();
    } finally {
      a.remove();
      URL.revokeObjectURL(url);
    }
  }
  async login(
    username: string,
    password: string,
    code?: string,
    options: { captcha?: Record<string, string>; signal?: AbortSignal } = {},
  ) {
    const proof = options.captcha || {};
    const allowedProof = new Set([
      "captcha_token",
      "geetest_challenge",
      "geetest_validate",
      "geetest_seccode",
      "captcha_id",
      "lot_number",
      "pass_token",
      "gen_time",
      "captcha_output",
    ]);
    if (
      (this.area !== "merchant" && Object.keys(proof).length) ||
      Object.entries(proof).some(
        ([key, value]) => !allowedProof.has(key) || typeof value !== "string",
      )
    )
      throw Error("登录验证码字段无效");
    const area = this.area,
      csrf = this.csrfToken;
    const s = await this.request<Session>("/login", {
      method: "POST",
      body: { ...proof, username, password, ...(code ? { code } : {}) },
      signal: options.signal,
    });
    if (
      options.signal?.aborted ||
      area !== this.area ||
      csrf !== this.csrfToken
    )
      throw Error("登录上下文已变化，请重新检查登录状态");
    this.csrfToken = s.csrf_token;
    return s;
  }
  async logout() {
    await this.request("/logout", { method: "POST", body: {} });
    this.csrfToken = "";
  }
  async merchantKeyLogin(
    merchantID: string,
    apiKey: string,
    code: string,
    signal?: AbortSignal,
  ) {
    if (
      this.area !== "merchant" ||
      !isPersonalMerchantID(merchantID) ||
      !apiKey ||
      apiKey.length > 512
    )
      throw new Error("请填写完整商户ID和商户密钥");
    const stamp = this.csrfToken;
    const value = await this.request<unknown>("/key-login", {
      method: "POST",
      body: { merchant_id: merchantID, api_key: apiKey, code },
      signal,
    });
    if (stamp !== this.csrfToken || this.area !== "merchant")
      throw new Error("登录上下文已变化，请重新检查登录状态");
    const s = parseMerchantKeySession(value, merchantID);
    this.csrfToken = s.csrf_token;
    return s;
  }
}

// A decoded, request-bound console proof is narrower than an HTTP 409. Legacy
// conflicts, malformed responses and mismatched identities remain uncertain.
function workflowRefusalProof212(
  status: number,
  path: string,
  body: unknown,
  data: unknown,
): ApiError["requestNotRecorded"] {
  const match = /^\/workflows\/(settlements|internal-transfers)$/.exec(path);
  if (
    status !== 409 ||
    !match ||
    !body ||
    typeof body !== "object" ||
    !data ||
    typeof data !== "object"
  )
    return;
  const b = body as Record<string, unknown>,
    d = data as Record<string, unknown>;
  if (
    d.request_not_recorded !== true ||
    d.kind !== match[1] ||
    typeof d.external_no !== "string" ||
    d.external_no !== b.external_no ||
    typeof d.merchant_id !== "string" ||
    !/^[1-9]\d{0,18}$/.test(d.merchant_id) ||
    !["conflict", "insufficient"].includes(String(d.reason)) ||
    (b.merchant_id !== undefined && b.merchant_id !== d.merchant_id)
  )
    return;
  return {
    kind: String(d.kind),
    merchant_id: d.merchant_id,
    external_no: d.external_no,
    reason: String(d.reason),
  };
}
export const validAmount = (x: string) =>
  /^(0|[1-9]\d{0,13})(\.\d{1,2})?$/.test(x) && /[1-9]/.test(x);
export function displayValue(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
