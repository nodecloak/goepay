// Read/validate the SMS gateway view from /settings/sms-channels. Secrets are
// never present; the view carries only configured/template/sign/masked principal.

export interface SmsProviderView {
  provider: "aliyun" | "qcloud" | "smsbao";
  configured: boolean;
  template: string;
  sign: string;
  principal: string;
}

export interface SmsGateway134 {
  providers: SmsProviderView[];
  active: string;
  status: number;
  fingerprint: string;
}

const PROVIDERS = ["aliyun", "qcloud", "smsbao"] as const;

export const smsProviderTitles: Record<string, string> = {
  aliyun: "阿里云短信",
  qcloud: "腾讯云短信",
  smsbao: "短信宝",
};

export function readSmsGateway134(raw: unknown): SmsGateway134 {
  if (!raw || typeof raw !== "object") throw new Error("invalid sms gateway");
  const o = raw as Record<string, unknown>;
  if (
    typeof o.fingerprint !== "string" ||
    o.fingerprint.length !== 64 ||
    typeof o.status !== "number" ||
    (o.status !== 0 && o.status !== 1) ||
    !Array.isArray(o.providers)
  )
    throw new Error("invalid sms gateway");
  const providers = o.providers.map((p) => {
    const m = p as Record<string, unknown>;
    if (
      typeof m.provider !== "string" ||
      !PROVIDERS.includes(m.provider as (typeof PROVIDERS)[number]) ||
      typeof m.configured !== "boolean"
    )
      throw new Error("invalid sms provider row");
    return {
      provider: m.provider as SmsProviderView["provider"],
      configured: m.configured,
      template: String(m.template ?? ""),
      sign: String(m.sign ?? ""),
      principal: String(m.principal ?? ""),
    };
  });
  return {
    providers,
    active: typeof o.active === "string" ? o.active : "",
    status: o.status,
    fingerprint: o.fingerprint,
  };
}
