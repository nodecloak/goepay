import QRCode from "qrcode";
export interface CollectionLayout {
  name: string;
  label: string;
  enabled: boolean;
  sort: number;
  url: string;
  img_width: number;
  img_height: number;
  qr_width: number;
  qr_height: number;
  qr_left: number;
  qr_top: number;
  foreground: string;
  background: string;
  font: string;
  font_color: string;
  rec_name_left: number | null;
  rec_name_top: number;
  word: number;
}
export interface CollectionStyleDraft {
  name: string;
  label: string;
  enabled: boolean;
  sort: string;
  url: string;
  img_width: string;
  img_height: string;
  qr_width: string;
  qr_height: string;
  qr_left: string;
  qr_top: string;
  foreground: string;
  background: string;
  font: string;
  font_color: string;
  rec_name_left: string;
  rec_name_top: string;
  word: string;
}
export interface CollectionStyleView {
  id: string;
  revision: string;
  style: CollectionLayout | null;
  fields: Record<string, unknown>;
}
export const newCollectionStyle = (): CollectionStyleDraft => ({
  name: "",
  label: "",
  enabled: true,
  sort: "0",
  url: "",
  img_width: "900",
  img_height: "1200",
  qr_width: "380",
  qr_height: "380",
  qr_left: "245",
  qr_top: "326",
  foreground: "#555555",
  background: "#FFFFFF",
  font: "60px '黑体'",
  font_color: "#FFFFFF",
  rec_name_left: "",
  rec_name_top: "210",
  word: "2",
});
const color = /^#[0-9a-fA-F]{6}$/,
  font =
    /^([1-9][0-9]{0,2})px ('[\p{L}\p{N} _-]{1,64}'|"[\p{L}\p{N} _-]{1,64}"|[\p{L}\p{N} _-]{1,64})$/u;
export function styleImageURL(value: unknown): string {
  if (typeof value !== "string") return "";
  if (/^\/collection-style-assets\/[a-f0-9]{64}$/.test(value)) return value;
  try {
    const u = new URL(value);
    return u.protocol === "https:" &&
      !u.username &&
      !u.password &&
      !u.hash &&
      value.length <= 2048 &&
      !/[\x00\r\n]/.test(value)
      ? u.href
      : "";
  } catch {
    return "";
  }
}
export function readCollectionLayout(raw: unknown): CollectionLayout {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("收款码样式数据不完整");
  const v = raw as Record<string, unknown>;
  const text = (k: string) =>
    typeof v[k] === "string" ? (v[k] as string) : "";
  const n = (k: string, min: number, max: number) => {
    const s = String(v[k]);
    if (
      !/^(0|[1-9]\d*)$/.test(s) ||
      !Number.isSafeInteger(Number(s)) ||
      Number(s) < min ||
      Number(s) > max
    )
      throw Error("样式数值无效：" + k);
    return Number(s);
  };
  const l: CollectionLayout = {
    name: text("name"),
    label: text("label"),
    enabled: v.enabled === true,
    sort: n("sort", 0, 1000000),
    url: styleImageURL(v.url),
    img_width: n("img_width", 64, 4096),
    img_height: n("img_height", 64, 4096),
    qr_width: n("qr_width", 32, 4096),
    qr_height: n("qr_height", 32, 4096),
    qr_left: n("qr_left", 0, 4096),
    qr_top: n("qr_top", 0, 4096),
    foreground: text("foreground"),
    background: text("background"),
    font: text("font"),
    font_color: text("font_color"),
    rec_name_left:
      v.rec_name_left === null ||
      v.rec_name_left === undefined ||
      v.rec_name_left === ""
        ? null
        : n("rec_name_left", 0, 4096),
    rec_name_top: n("rec_name_top", 0, 4096),
    word: n("word", 0, 2),
  };
  if (
    !/^[A-Za-z][A-Za-z0-9_]{0,99}$/.test(l.name) ||
    !l.label ||
    l.label.length > 100 ||
    !l.url ||
    !color.test(l.foreground) ||
    (l.background !== "transparent" && !color.test(l.background)) ||
    (l.font_color !== "transparent" && !color.test(l.font_color)) ||
    !font.test(l.font) ||
    !font.exec(l.font)![2]!.replace(/['"]/g, "").trim() ||
    Number(font.exec(l.font)![1]) > 256
  )
    throw Error("请核对风格标识、显示名称、图片地址、字体或颜色");
  if (
    l.img_width * l.img_height > 4194304 ||
    l.qr_left + l.qr_width > l.img_width ||
    l.qr_top + l.qr_height > l.img_height ||
    l.rec_name_top > l.img_height ||
    (l.rec_name_left !== null && l.rec_name_left > l.img_width)
  )
    throw Error("二维码或文字超出画布，画布最多4MP");
  return l;
}
export function styleDraft(view: CollectionStyleView): CollectionStyleDraft {
  const draft = newCollectionStyle(),
    source = view.style || view.fields;
  for (const k of Object.keys(draft) as (keyof CollectionStyleDraft)[]) {
    const value = source[k];
    if (k === "enabled") draft.enabled = value === true;
    else if (value !== undefined)
      draft[k] = value === null ? "" : String(value);
  }
  if (!view.style) {
    draft.label = String(view.fields.title || view.fields.name || "");
    draft.foreground = String(view.fields.text_color || draft.foreground);
  }
  return draft;
}
export function collectionStylePayload(draft: CollectionStyleDraft) {
  readCollectionLayout(draft);
  const payload = { ...draft };
  for (const key of Object.keys(payload) as (keyof CollectionStyleDraft)[]) {
    if (key !== "enabled")
      payload[key] = payload[key] === null ? "" : String(payload[key]);
  }
  return payload;
}
export function readCollectionStyleID(value: unknown): string | undefined {
  if (value === undefined) return;
  return typeof value === "string" &&
    /^[1-9]\d{0,18}$/.test(value) &&
    BigInt(value) <= 9223372036854775807n
    ? value
    : "";
}
export async function collectionBackground(
  address: string,
  signal?: AbortSignal,
): Promise<HTMLImageElement> {
  if (
    !styleImageURL(address) &&
    !/^\/console\/v1\/admin\/collection-style-images\/[a-f0-9]{64}$/.test(
      address,
    )
  )
    throw Error("背景图地址无效");
  return new Promise((resolve, reject) => {
    const img = new Image();
    let done = false;
    const finish = (e?: Error) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      img.onload = null;
      img.onerror = null;
      if (e) {
        img.src = "";
        reject(e);
      } else resolve(img);
    };
    const abort = () => finish(Error("预览已取消"));
    const timer = setTimeout(
      () => finish(Error("背景图读取超时，请上传本地图片或检查跨域访问")),
      8000,
    );
    img.crossOrigin = "anonymous";
    img.onload = () => {
      if (
        img.naturalWidth < 1 ||
        img.naturalHeight < 1 ||
        img.naturalWidth > 4096 ||
        img.naturalHeight > 4096 ||
        img.naturalWidth * img.naturalHeight > 4194304
      )
        finish(Error("背景图超过4MP或4096像素限制"));
      else finish();
    };
    img.onerror = () =>
      finish(Error("背景图读取失败；HTTPS图片需允许跨域读取，可改用上传"));
    signal?.addEventListener("abort", abort, { once: true });
    if (signal?.aborted) {
      abort();
      return;
    }
    img.src = address;
  });
}
export async function renderCollectionPoster(
  layout: CollectionLayout,
  address: string,
  merchant: string,
  options: { imageSource?: string; signal?: AbortSignal } = {},
): Promise<string> {
  const l = readCollectionLayout(layout),
    canvas = document.createElement("canvas");
  canvas.width = l.img_width;
  canvas.height = l.img_height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw Error("浏览器无法创建图片画布");
  const bg = await collectionBackground(
    options.imageSource || l.url,
    options.signal,
  );
  if (options.signal?.aborted) throw Error("预览已取消");
  ctx.drawImage(bg, 0, 0, canvas.width, canvas.height);
  const qr = document.createElement("canvas"),
    side = Math.min(l.qr_width, l.qr_height);
  await QRCode.toCanvas(qr, address, {
    width: side,
    margin: 2,
    color: {
      dark: l.foreground,
      light: l.background === "transparent" ? "#00000000" : l.background,
    },
    errorCorrectionLevel: "M",
  });
  if (options.signal?.aborted) throw Error("预览已取消");
  ctx.drawImage(
    qr,
    l.qr_left + (l.qr_width - side) / 2,
    l.qr_top + (l.qr_height - side) / 2,
    side,
    side,
  );
  if (l.word > 0 && l.font_color !== "transparent") {
    ctx.font = l.font;
    ctx.fillStyle = l.font_color;
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    ctx.fillText(
      l.word === 1 ? merchant : `扫码向 ${merchant} 付款`,
      l.rec_name_left ?? l.img_width / 2,
      l.rec_name_top,
      l.img_width,
    );
  }
  try {
    return canvas.toDataURL("image/png");
  } catch {
    throw Error("背景图不允许导出，请上传本地图片或配置图片跨域访问");
  }
}
export async function uploadCollectionBackground(
  client: { area: string; csrfToken: string; onUnauthorized?: () => void },
  file: File,
  signal?: AbortSignal,
) {
  if (
    client.area !== "admin" ||
    !client.csrfToken ||
    file.size < 1 ||
    file.size > 1048576 ||
    !["image/png", "image/jpeg"].includes(file.type)
  )
    throw Error("请选择不超过1MiB的PNG或JPEG图片");
  const form = new FormData();
  form.set("file", file);
  const response = await fetch("/console/v1/admin/collection-style-images", {
    method: "POST",
    headers: { "X-CSRF-Token": client.csrfToken },
    credentials: "same-origin",
    body: form,
    signal,
  });
  if (response.status === 401) client.onUnauthorized?.();
  const out = await response.json();
  if (!response.ok || out.code !== 0)
    throw Error(out.message || "背景图上传失败");
  const a = out.data;
  if (
    !a ||
    typeof a.id !== "string" ||
    !/^[a-f0-9]{64}$/.test(a.id) ||
    !Number.isSafeInteger(a.width) ||
    !Number.isSafeInteger(a.height) ||
    a.width < 1 ||
    a.height < 1 ||
    a.width > 4096 ||
    a.height > 4096 ||
    a.width * a.height > 4194304
  )
    throw Error("背景图响应校验失败");
  return a as { id: string; width: number; height: number };
}
