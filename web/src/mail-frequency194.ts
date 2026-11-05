export const mailFields194 = [
  ["mail_interval_email", "同一邮箱发送间隔(秒)", 86400],
  ["mail_interval_ip", "同一IP发送间隔(秒)", 86400],
  ["mail_daily_email", "同一邮箱每日发送次数", 100000],
  ["mail_daily_ip", "同一IP每日发送次数", 100000],
] as const;
export type MailKey194 = (typeof mailFields194)[number][0];
export type MailValues194 = Record<MailKey194, string>;
export type MailPolicy194 = MailValues194 & {
  effective: MailValues194;
  configured: boolean;
  revision: string;
  fingerprint: string;
};
export function mailValues194(v: unknown, legacy = false): MailValues194 {
  if (!v || typeof v !== "object")
    throw Error("邮件频率配置无法确认，请刷新页面后核对");
  const out = {} as MailValues194;
  for (const [key, , max] of mailFields194) {
    const raw = (v as Record<string, unknown>)[key];
    if (
      typeof raw !== "string" ||
      !/^(0|[1-9]\d*)$/.test(raw) ||
      Number(raw) > max ||
      (raw === "0" && !(legacy && key === "mail_interval_ip"))
    )
      throw Error("邮件频率配置无效，请填写范围内的正整数");
    out[key] = raw;
  }
  return out;
}
export function readMailPolicy194(v: unknown): MailPolicy194 {
  const fields = mailValues194(v),
    r = v as Record<string, unknown>;
  if (
    typeof r.configured !== "boolean" ||
    typeof r.revision !== "string" ||
    !/^(0|[1-9]\d*)$/.test(r.revision) ||
    r.configured !== (r.revision !== "0") ||
    typeof r.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(r.fingerprint)
  )
    throw Error("邮件频率配置无法确认，请刷新页面后核对");
  const effective = mailValues194(r.effective, !r.configured);
  if (r.configured && mailFields194.some(([k]) => fields[k] !== effective[k]))
    throw Error("邮件频率生效状态无法确认，请刷新页面后核对");
  return {
    ...fields,
    effective,
    configured: r.configured,
    revision: r.revision,
    fingerprint: r.fingerprint,
  };
}
