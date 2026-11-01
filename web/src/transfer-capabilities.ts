import {
  readFundsAvailability,
  type FundsAvailability,
} from "./funds-availability";
import { cents } from "./transfers";

interface TransferCapabilityCommon {
  writes_enabled: boolean;
  platform_enabled: boolean;
  effective_enabled?: boolean;
  permission_source?: string;
  merchant_enabled: boolean;
  channels: {
    id: string;
    types: string[];
    bank_product?: string;
    bank_code_required?: boolean;
  }[];
  minimum_money?: string;
  maximum_money?: string;
  daily_recipient_limit?: number;
  default_remark?: string;
  default_title?: string;
}
export type QuotedTransferCapabilities = TransferCapabilityCommon &
  FundsAvailability & {
    policy_available: true;
    transfer_rate: string;
  };
export type ReadonlyTransferCapabilities = TransferCapabilityCommon & {
  policy_available: false;
  writes_enabled: false;
  ledger_available_money: string;
  reserved_money: string;
  transfer_rate?: never;
  available_money?: never;
  today_blocked_money?: never;
  settle_type?: never;
};
export type TransferCapabilities =
  QuotedTransferCapabilities | ReadonlyTransferCapabilities;

/** A missing pricing policy is not a zero fee or a spendable ledger balance. */
export function readTransferCapabilities(value: unknown): TransferCapabilities {
  const invalid = () => new Error("服务端资金政策或余额信息不完整，请重新查询");
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw invalid();
  const data = value as Record<string, unknown>;
  if (typeof data.writes_enabled !== "boolean" || !Array.isArray(data.channels))
    throw invalid();
  if (
    data.channels.some(
      (c) =>
        !c ||
        typeof c !== "object" ||
        typeof c.id !== "string" ||
        !Array.isArray(c.types) ||
        c.types.some((t: unknown) => typeof t !== "string"),
    )
  )
    throw invalid();
  const common = data as unknown as TransferCapabilityCommon;
  const hasFeeFlag = Object.hasOwn(data, "fee_policy_available"),
    hasFundsFlag = Object.hasOwn(data, "funds_policy_available");
  if (hasFeeFlag || hasFundsFlag) {
    if (
      data.fee_policy_available === false &&
      data.funds_policy_available === false &&
      data.writes_enabled === false &&
      data.effective_enabled === false &&
      data.permission_source === "group_disabled" &&
      typeof data.platform_enabled === "boolean" &&
      typeof data.merchant_enabled === "boolean"
    ) {
      const money = (key: string) => {
        const raw = data[key];
        if (
          typeof raw !== "string" ||
          !/^(0|[1-9]\d{0,16})\.\d{2}$/.test(raw) ||
          BigInt(raw.replace(".", "")) > 9223372036854775807n
        )
          throw invalid();
        return raw;
      };
      return {
        ...common,
        policy_available: false,
        writes_enabled: false,
        ledger_available_money: money("ledger_available_money"),
        reserved_money: money("reserved_money"),
        // Ignore any stray derived fields in a degraded response.
        transfer_rate: undefined,
        available_money: undefined,
        today_blocked_money: undefined,
        settle_type: undefined,
      };
    }
    if (
      data.fee_policy_available !== true ||
      data.funds_policy_available !== true
    )
      throw invalid();
  }
  // Compatibility applies only when BOTH flags are absent and the original
  // complete DTO still passes the same real funds and rate validation.
  const rate = data.transfer_rate;
  if (
    typeof rate !== "string" ||
    cents(rate) === undefined ||
    cents(rate)! > 10000n
  )
    throw invalid();
  return {
    ...common,
    ...readFundsAvailability(data),
    transfer_rate: rate,
    policy_available: true,
  };
}
