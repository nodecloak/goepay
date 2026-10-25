import { ApiError } from "./api";
export interface EnrollmentCapabilities {
  review_required: boolean;
  registration_open: boolean;
  email_enabled: boolean;
  invite_required: boolean;
  expires_seconds: number;
}
export function parseEnrollmentCapabilities(
  value: unknown,
): EnrollmentCapabilities {
  const x = value as Partial<EnrollmentCapabilities> | null;
  if (
    !x ||
    typeof x.registration_open !== "boolean" ||
    typeof x.email_enabled !== "boolean" ||
    typeof x.invite_required !== "boolean" ||
    (x.review_required !== undefined &&
      typeof x.review_required !== "boolean") ||
    !Number.isInteger(x.expires_seconds) ||
    Number(x.expires_seconds) <= 0
  )
    throw new Error("服务能力响应不完整，暂不能提交");
  return {
    ...x,
    review_required: x.review_required ?? false,
  } as EnrollmentCapabilities;
}
export function registrationNotice(value: unknown): string {
  const result = value as Record<string, unknown> | null;
  if (
    !result ||
    typeof result.merchant_id !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(result.merchant_id) ||
    typeof result.username !== "string" ||
    !result.username ||
    result.username.length > 100 ||
    (Object.hasOwn(result, "pay") &&
      !["0", "1", "2"].includes(result.pay as string))
  )
    throw new Error("注册结果尚未确认，请勿直接重复提交；可返回登录核对");
  const review =
    result.pay === "2"
      ? "收款权限待审核，请等待管理员审核。"
      : result.pay === "0"
        ? "当前禁止收款，请联系管理员核对。"
        : "";
  return `注册完成，商户 ID：${result.merchant_id}。${review}请返回登录，不会自动建立会话。`;
}
export function enrollmentError(status: number, message: string): string {
  if (status === 429)
    return "请求过于频繁，请稍后重试；服务端未提供准确恢复时间。";
  if (status === 503)
    return "邮件或账户服务当前不可用，未确认发送或完成，请稍后重新核对。";
  if (status === 422)
    return "邮箱、验证码或表单无效；请检查验证码是否正确、过期或已被新验证码替代。";
  if (status === 409) return "账户资料冲突，请检查用户名与邮箱。";
  if (status === 403)
    return "当前操作未开放或请求来源不被允许，请重新读取服务能力。";
  return message || "请求结果尚未确认，请稍后核对";
}
export async function publicRequest<T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(`/console/v1/public/${path}`, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });
  let payload: { code: number; message?: string; data: T };
  try {
    payload = await response.json();
  } catch {
    throw new ApiError("服务器返回格式错误，请稍后核对", response.status, -1);
  }
  if (!response.ok || payload.code !== 0)
    throw new ApiError(
      enrollmentError(response.status, payload.message || "请求失败"),
      response.status,
      payload.code,
    );
  return payload.data;
}
export const utf8Length = (value: string) =>
  new TextEncoder().encode(value).length;
