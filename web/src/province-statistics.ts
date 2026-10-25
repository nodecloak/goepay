import boundaries from "./data/province-boundaries.json";
export function provinceRange(
  preset: string,
  now = new Date(),
): { from: string; to: string } | null {
  const days: Record<string, number> = {
    today: 0,
    yesterday: 0,
    "3days": 2,
    "7days": 6,
    "15days": 14,
    "30days": 29,
  };
  if (!Object.hasOwn(days, preset)) return null;
  const today = new Date(now.getTime() + 8 * 3600000)
      .toISOString()
      .slice(0, 10),
    end = Date.parse(today) + (preset === "yesterday" ? -86400000 : 0);
  return {
    from: new Date(end - days[preset] * 86400000).toISOString().slice(0, 10),
    to: new Date(end).toISOString().slice(0, 10),
  };
}
export function provinceStatisticsQuery(
  area: string,
  preset: string,
  uid: string,
  channel: string,
): { query?: URLSearchParams; error?: string } {
  const range = provinceRange(preset);
  if (!range) return { error: "请选择有效日期范围" };
  if (area !== "admin" && (uid !== "" || channel !== ""))
    return { error: "商户不能选择其它商户或通道" };
  const id = (raw: string) =>
    raw === "" ||
    (/^[1-9]\d{0,18}$/.test(raw) && BigInt(raw) <= 9223372036854775807n);
  if (!id(uid) || !id(channel))
    return { error: "商户号和通道ID须为有效正整数" };
  const query = new URLSearchParams({ view: "original", ...range });
  if (uid) query.set("uid", uid);
  if (channel) query.set("channel", channel);
  return { query };
}
function amount(value: unknown): bigint {
  if (typeof value !== "string" || !/^(0|[1-9]\d{0,37})\.\d{2}$/.test(value))
    throw Error("地区统计金额格式不正确");
  const [w, c] = value.split(".");
  return BigInt(w) * 100n + BigInt(c);
}
function count(value: unknown): bigint {
  if (typeof value !== "string" || !/^(0|[1-9]\d{0,37})$/.test(value))
    throw Error("地区统计笔数格式不正确");
  return BigInt(value);
}
const money = (n: bigint) =>
  (n / 100n).toString() + "." + (n % 100n).toString().padStart(2, "0");
export function provinceMoney(value: unknown): string {
  amount(value);
  return String(value);
}
export function provinceCount(value: unknown): string {
  count(value);
  return String(value);
}
export interface ProvinceRegion {
  name: string;
  money: string;
  count: string;
  colour: string;
}
const aliases = new Map<string, string>();
for (const b of boundaries) {
  aliases.set(b.name, b.name);
  aliases.set(
    b.name.replace(
      /壮族自治区$|回族自治区$|维吾尔自治区$|自治区$|特别行政区$|省$|市$/,
      "",
    ),
    b.name,
  );
}
export function provinceStatisticsData(rows: Record<string, unknown>[]) {
  if (!Array.isArray(rows)) throw Error("地区统计数据格式不正确");
  const sums = new Map(
    boundaries.map((b) => [b.name, { amount: 0n, count: 0n }]),
  );
  let unmappedAmount = 0n,
    unmappedCount = 0n;
  const unmapped: ProvinceRegion[] = [];
  for (const row of rows) {
    if (typeof row.name !== "string") throw Error("地区名称未记录");
    const a = amount(row.money),
      n = count(row.count),
      name = aliases.get(row.name),
      target = name ? sums.get(name) : undefined;
    if (target) {
      target.amount += a;
      target.count += n;
    } else {
      unmappedAmount += a;
      unmappedCount += n;
      unmapped.push({
        name: row.name,
        money: money(a),
        count: n.toString(),
        colour: "#f0f5ff",
      });
    }
  }
  let max = 0n;
  for (const v of sums.values()) if (v.amount > max) max = v.amount;
  const palette = ["#e0f3ff", "#80bdff", "#409eff", "#1a6fd1", "#0d3d80"];
  const regions: ProvinceRegion[] = [...sums].map(([name, v]) => ({
    name,
    money: money(v.amount),
    count: v.count.toString(),
    colour:
      v.count === 0n
        ? "#f0f5ff"
        : palette[Number((v.amount * 5n) / (max + 1n))],
  }));
  const rank = regions
    .filter((r) => r.count !== "0")
    .sort((a, b) => {
      const x = amount(a.money),
        y = amount(b.money);
      return x === y ? a.name.localeCompare(b.name) : x > y ? -1 : 1;
    })
    .slice(0, 10);
  return {
    regions,
    rank,
    unmappedMoney: money(unmappedAmount),
    unmappedCount: unmappedCount.toString(),
    unmapped,
  };
}
let cachedPaths: ReadonlyArray<{ name: string; path: string }> | undefined;
export function provincePaths() {
  if (cachedPaths) return cachedPaths;
  const project = (p: number[]) => [
    (p[0] * Math.PI) / 180,
    Math.log(Math.tan(Math.PI / 4 + (p[1] * Math.PI) / 360)),
  ];
  let minX = Infinity,
    maxX = -Infinity,
    minY = Infinity,
    maxY = -Infinity;
  for (const b of boundaries)
    for (const ring of b.rings)
      for (const p of ring) {
        const [x, y] = project(p);
        minX = Math.min(minX, x);
        maxX = Math.max(maxX, x);
        minY = Math.min(minY, y);
        maxY = Math.max(maxY, y);
      }
  const scale = Math.min(672 / (maxX - minX), 492 / (maxY - minY)),
    left = (720 - (maxX - minX) * scale) / 2,
    top = (540 - (maxY - minY) * scale) / 2;
  cachedPaths = Object.freeze(
    boundaries.map((b) =>
      Object.freeze({
        name: b.name,
        path: b.rings
          .map(
            (ring) =>
              ring
                .map((p, i) => {
                  const [x, y] = project(p);
                  return (
                    (i === 0 ? "M" : "L") +
                    (left + (x - minX) * scale).toFixed(2) +
                    "," +
                    (top + (maxY - y) * scale).toFixed(2)
                  );
                })
                .join("") + "Z",
          )
          .join(""),
      }),
    ),
  );
  return cachedPaths;
}
