import type { ResourceField } from "./resources";
export const businessScenes: Record<string, string> = {
  regaudit: "新商户待审核",
  apply: "手动提现申请",
  domain: "授权域名待审核",
  internal_transfer: "站内转账待审核",
  complain_all: "全部交易投诉",
  channellimit: "支付通道达到当日限额",
};
export const businessStates: Record<string, string> = {
  not_scheduled: "未安排发送",
  queued: "等待处理",
  accepted: "已受理",
  unknown: "待核实",
  not_sent: "未发送",
};
export const businessModes = [
  { value: "0", label: "关闭提醒" },
  { value: "1", label: "电子邮件" },
  { value: "2", label: "群机器人" },
  { value: "3", label: "短信" },
  { value: "4", label: "Telegram" },
  { value: "5", label: "QQ Bot" },
];
export function businessNoticeSettingsFields(
  fields: ResourceField[],
  key: unknown,
): ResourceField[] {
  if (
    typeof key !== "string" ||
    ![
      "msgconfig_regaudit",
      "msgconfig_apply",
      "msgconfig_domain",
      "msgconfig_complain_all",
      "msgconfig_channellimit",
    ].includes(key)
  )
    return fields;
  return fields.map((field) =>
    field.name === "value"
      ? {
          ...field,
          type: "select",
          options: businessModes
            .filter((o) => o.value !== "3" || key === "msgconfig_complain_all")
            .map((o) => ({ ...o })),
        }
      : field,
  );
}
export interface BusinessNotice {
  id: string;
  merchant_id: string;
  source_id: string;
  job_id: string;
  scene: string;
  mode: string;
  transport: string;
  state: string;
  at: string;
}
const id = (value: unknown, zero = false): value is string =>
  typeof value === "string" &&
  /^(0|[1-9][0-9]{0,18})$/.test(value) &&
  (zero || value !== "0") &&
  BigInt(value) <= 9223372036854775807n;
export function parseBusinessNotices(value: unknown): {
  items: BusinessNotice[];
  next_cursor: string;
} {
  if (!value || typeof value !== "object") throw Error("invalid history");
  const v = value as Record<string, unknown>;
  if (
    !Array.isArray(v.items) ||
    v.items.length > 100 ||
    (v.next_cursor !== "" && !id(v.next_cursor))
  )
    throw Error("invalid history");
  const items = v.items.map((raw: unknown): BusinessNotice => {
    if (!raw || typeof raw !== "object") throw Error("invalid event");
    const e = raw as Record<string, unknown>;
    for (const k of ["id", "source_id"])
      if (!id(e[k])) throw Error("invalid identity");
    if (
      !id(
        e.merchant_id,
        ["complain_all", "channellimit"].includes(String(e.scene)),
      )
    )
      throw Error("invalid merchant identity");
    if (!id(e.job_id, true)) throw Error("invalid job");
    if (
      typeof e.scene !== "string" ||
      !Object.hasOwn(businessScenes, e.scene) ||
      typeof e.state !== "string" ||
      !Object.hasOwn(businessStates, e.state) ||
      typeof e.mode !== "string" ||
      !businessModes.some((m) => m.value === e.mode) ||
      typeof e.transport !== "string" ||
      !["", "smtp", "robot", "sms", "telegram", "qqbot"].includes(
        e.transport,
      ) ||
      (e.mode === "3" && e.scene !== "complain_all") ||
      typeof e.at !== "string" ||
      !Number.isFinite(Date.parse(e.at))
    )
      throw Error("invalid event");
    if (
      (e.mode === "0") !== (e.job_id === "0") ||
      (e.mode === "0") !== (e.transport === "")
    )
      throw Error("invalid route");
    if (
      (
        {
          "0": "",
          "1": "smtp",
          "2": "robot",
          "3": "sms",
          "4": "telegram",
          "5": "qqbot",
        } as Record<string, string>
      )[e.mode] !== e.transport
    )
      throw Error("invalid mode transport");
    return Object.fromEntries(
      [
        "id",
        "merchant_id",
        "source_id",
        "job_id",
        "scene",
        "mode",
        "transport",
        "state",
        "at",
      ].map((k) => [k, e[k]]),
    ) as unknown as BusinessNotice;
  });
  return { items, next_cursor: v.next_cursor as string };
}
