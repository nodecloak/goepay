import { onBeforeUnmount, onMounted, ref } from "vue";

export const siteKeys191 = [
  "sitename",
  "title",
  "keywords",
  "description",
  "orgname",
  "email",
  "kfqq",
  "kfqq_url",
  "wework_url",
  "qqqun",
  "login_subtitle",
  "homepage",
  "homepage_url",
] as const;
export type PublicSite191 = Record<(typeof siteKeys191)[number], string>;
export function readPublicSite191(value: unknown): PublicSite191 {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("网站信息格式无效");
  const raw = value as Record<string, unknown>,
    out = {} as PublicSite191;
  for (const key of siteKeys191) {
    const v = raw[key];
    if (
      typeof v !== "string" ||
      new TextEncoder().encode(v).length > 1000 ||
      /[\u0000-\u001f\u007f-\u009f]/.test(v) ||
      v.trim() !== v
    )
      throw Error("网站信息格式无效");
    out[key] = v;
  }
  if (
    !out.sitename ||
    Array.from(out.sitename).length > 64 ||
    !out.title ||
    !["0", "1", "2", "3"].includes(out.homepage)
  )
    throw Error("网站信息格式无效");
  for (const key of [
    "kfqq_url",
    "wework_url",
    "qqqun",
    "homepage_url",
  ] as const) {
    if (!out[key]) continue;
    const u = new URL(out[key]);
    if (
      !["http:", "https:"].includes(u.protocol) ||
      u.username ||
      u.password ||
      /[\\"<>]/.test(out[key])
    )
      throw Error("网站链接无效");
  }
  if (out.homepage === "2" && !out.homepage_url) throw Error("首页地址未配置");
  return out;
}

export function usePublicSite191() {
  const site = ref<PublicSite191>(),
    active = ref(true);
  let controller: AbortController | undefined,
    generation = 0;
  async function reload() {
    if (!active.value) return;
    controller?.abort();
    const current = ++generation;
    const c = new AbortController();
    controller = c;
    try {
      const r = await fetch("/console/v1/public/site", {
        credentials: "omit",
        headers: { Accept: "application/json" },
        signal: c.signal,
      });
      if (!r.ok) return;
      const body = await r.json();
      if (body.code !== 0) return;
      const value = readPublicSite191(body.data);
      if (active.value && !c.signal.aborted && current === generation)
        site.value = value;
    } catch {
      /* Keep the current public brand if this optional read fails. */
    }
  }
  onMounted(() => {
    void reload();
  });
  onBeforeUnmount(() => {
    active.value = false;
    generation++;
    controller?.abort();
  });
  return { site, reload };
}
