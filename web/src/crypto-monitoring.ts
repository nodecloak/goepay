const networks = ["tron", "ethereum"] as const;
type Network = (typeof networks)[number];

export const cryptoUsageStates = {
  pending: "待付款",
  underpaid: "少付待核对",
  paid: "已到账",
  overpaid: "超付待核对",
  expired: "已过期",
  late: "逾期到账待核对",
} as const;

type DepositPool = {
  network: Network;
  enabled: boolean;
  deposit_total: number;
  deposit_used: number;
  deposit_available: number;
};
type ChannelCapacity = {
  id: string;
  name: string;
  network: Network;
  enabled: boolean;
  archived: boolean;
  day_limit: string;
  day_used: string;
  day_remaining: string | null;
  day_usage_by_state: Record<keyof typeof cryptoUsageStates, string>;
};
export type CryptoMonitoring = {
  as_of: string;
  window_start: string;
  window_end: string;
  networks: DepositPool[];
  channels: ChannelCapacity[];
};

const invalid = () => new Error("USDT 监控数据无法核实");

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw invalid();
  return value as Record<string, unknown>;
}

function utc(value: unknown) {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z$/.test(value) ||
    !Number.isFinite(Date.parse(value))
  )
    throw invalid();
  return Date.parse(value);
}

function amountUnits(value: unknown) {
  if (
    typeof value !== "string" ||
    !/^(?:0|[1-9]\d*)(?:\.\d{1,6})?$/.test(value)
  )
    throw invalid();
  const [whole, decimal = ""] = value.split(".");
  return BigInt(whole!) * 1_000_000n + BigInt(decimal.padEnd(6, "0"));
}

function count(value: unknown) {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    throw invalid();
  return value;
}

export function readCryptoMonitoring(raw: unknown): CryptoMonitoring {
  const result = record(raw);
  const asOf = utc(result.as_of),
    start = utc(result.window_start),
    end = utc(result.window_end);
  const day = 86_400_000;
  if (
    start % day !== 0 ||
    end - start !== day ||
    asOf < start ||
    asOf >= end ||
    !Array.isArray(result.networks) ||
    !Array.isArray(result.channels)
  )
    throw invalid();

  const seenNetworks = new Set<string>();
  for (const item of result.networks) {
    const row = record(item);
    if (
      !networks.includes(row.network as Network) ||
      typeof row.enabled !== "boolean" ||
      seenNetworks.has(row.network as string) ||
      count(row.deposit_used) + count(row.deposit_available) !==
        count(row.deposit_total)
    )
      throw invalid();
    seenNetworks.add(row.network as string);
  }
  if (seenNetworks.size !== networks.length) throw invalid();

  const seenChannels = new Set<string>();
  for (const item of result.channels) {
    const row = record(item);
    if (
      typeof row.id !== "string" ||
      !/^[1-9]\d{0,18}$/.test(row.id) ||
      BigInt(row.id) > 9223372036854775807n ||
      seenChannels.has(row.id) ||
      typeof row.name !== "string" ||
      !row.name.trim() ||
      typeof row.network !== "string" ||
      !seenNetworks.has(row.network) ||
      typeof row.enabled !== "boolean" ||
      typeof row.archived !== "boolean" ||
      (row.enabled && row.archived)
    )
      throw invalid();
    seenChannels.add(row.id);

    const limit = amountUnits(row.day_limit),
      used = amountUnits(row.day_used);
    const byState = record(row.day_usage_by_state);
    const total = Object.keys(cryptoUsageStates).reduce(
      (sum, key) => sum + amountUnits(byState[key]),
      0n,
    );
    if (total !== used) throw invalid();
    if (limit === 0n) {
      if (row.day_remaining !== null) throw invalid();
    } else if (
      amountUnits(row.day_remaining) !== (used < limit ? limit - used : 0n)
    ) {
      throw invalid();
    }
  }
  return result as unknown as CryptoMonitoring;
}
