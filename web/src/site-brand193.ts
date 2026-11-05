import { ApiClient, ApiError } from "./api";
export interface BrandAsset193 {
  id: string;
  mime: "image/png" | "image/jpeg";
  width: number;
  height: number;
}
export interface Brand193 {
  revision: string;
  fingerprint: string;
  logo: BrandAsset193 | null;
  icon: BrandAsset193 | null;
}
export function readBrand193(raw: unknown): Brand193 {
  const v = raw as Brand193;
  const asset = (x: BrandAsset193 | null) =>
    x === null ||
    (x &&
      /^[a-f0-9]{64}$/.test(x.id) &&
      ["image/png", "image/jpeg"].includes(x.mime) &&
      Number.isInteger(x.width) &&
      Number.isInteger(x.height) &&
      x.width > 0 &&
      x.height > 0 &&
      x.width <= 2048 &&
      x.height <= 2048 &&
      x.width * x.height <= 1048576);
  if (
    !v ||
    !/^(0|[1-9]\d*)$/.test(v.revision) ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
    !asset(v.logo) ||
    !asset(v.icon)
  )
    throw new Error("品牌图片状态无法确认");
  return {
    revision: v.revision,
    fingerprint: v.fingerprint,
    logo: v.logo,
    icon: v.icon,
  };
}
export async function uploadBrand193(
  client: ApiClient,
  slot: "logo" | "icon",
  file: File,
  fingerprint: string,
  signal: AbortSignal,
): Promise<Brand193> {
  const csrf = client.csrfToken;
  if (client.area !== "admin" || !csrf || !/^[a-f0-9]{64}$/.test(fingerprint))
    throw new Error("请刷新页面后核对本人管理员会话");
  if (
    file.size === 0 ||
    file.size >= 2 * 1024 * 1024 ||
    (file.type &&
      !["image/png", "image/jpeg", "image/jpg", "image/gif"].includes(
        file.type,
      ))
  )
    throw new Error("请选择小于2MB的PNG、JPEG或GIF图片");
  const form = new FormData();
  form.append("file", file);
  const reply = await fetch("/console/v1/admin/settings/branding/" + slot, {
    method: "POST",
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      "X-CSRF-Token": csrf,
      "X-Expected-Fingerprint": fingerprint,
    },
    body: form,
    signal,
  });
  if (signal.aborted || client.csrfToken !== csrf)
    throw new Error("会话已变化，请刷新页面后核对");
  let body;
  try {
    body = await reply.json();
  } catch {
    throw new Error("上传结果无法确认，请刷新页面后核对");
  }
  if (!reply.ok || body.code !== 0)
    throw new ApiError(
      reply.status === 409
        ? "图片设置已变化，请刷新页面后核对后上传"
        : reply.status === 422 || reply.status === 413
          ? "图片无效、尺寸或大小超限"
          : "上传结果无法确认，请刷新页面后核对",
      reply.status,
      body.code ?? -1,
    );
  return readBrand193(body.data);
}
export function refreshBrand193() {
  window.dispatchEvent(new Event("goepay-branding193-updated"));
}
