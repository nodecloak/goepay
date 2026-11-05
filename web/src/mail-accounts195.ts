export interface MailAccount195 {
  name: "smtp";
  title: "SMTP发信";
  status: 0 | 1;
  configured: boolean;
  config: { host: string; port: string; username: string };
  password_set: boolean;
  revision: string;
  fingerprint: string;
  test_available: boolean;
  effective_source: "deployment" | "console";
}
export function readMailAccount195(raw: unknown): MailAccount195 {
  const r = raw as MailAccount195;
  if (
    !r ||
    r.name !== "smtp" ||
    r.title !== "SMTP发信" ||
    (r.status !== 0 && r.status !== 1) ||
    typeof r.configured !== "boolean" ||
    typeof r.password_set !== "boolean" ||
    typeof r.test_available !== "boolean" ||
    !r.config ||
    typeof r.config.host !== "string" ||
    typeof r.config.port !== "string" ||
    typeof r.config.username !== "string" ||
    !Number.isSafeInteger(Number(r.config.port)) ||
    !/^[1-9]\d*$/.test(r.config.port) ||
    Number(r.config.port) > 65535 ||
    typeof r.revision !== "string" ||
    !/^(0|[1-9]\d*)$/.test(r.revision) ||
    r.configured !== (r.revision !== "0") ||
    typeof r.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(r.fingerprint) ||
    r.effective_source !== (r.configured ? "console" : "deployment") ||
    Object.hasOwn(r.config, "password")
  )
    throw Error("SMTP账号状态无法确认，请刷新页面后核对");
  return r;
}
