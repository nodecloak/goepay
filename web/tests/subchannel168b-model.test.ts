import { describe, it, expect } from "vitest";
import fixture from "../e2e/subchannel168b-fixture.json";
import {
  readSubchannelPage,
  readSubchannelRow,
  readSubchannelSchemas,
  readSubchannelPolicy,
  subchannelMoney,
  subchannelMoneyInput,
  subchannelSessionID,
  readSubchannelRoute,
  subchannelListQuery,
  subchannelOrdersHref,
} from "../src/subchannels";
const row = () => structuredClone(fixture.merchant.items[0]);
describe("subchannel168b source-bound DTO", () => {
  it("reads actual HTTP merchant/admin projections without rounding IDs", () => {
    const page = readSubchannelPage(fixture.merchant, "merchant", "1");
    expect(page.items[0]?.id).toBe("9007199254740993");
    expect(
      readSubchannelPage(fixture.admin, "admin", "168").items,
    ).toHaveLength(2);
    expect(readSubchannelSchemas(fixture.schemas)[0]?.parentId).toBe("7");
    expect(readSubchannelPolicy(fixture.policy, "1").rollType).toBe(2);
  });
  it("keeps actual empty page total and exact order association", () => {
    const empty = { ...fixture.merchant, items: [], page: 2 };
    expect(readSubchannelPage(empty, "merchant", "1").total).toBe(2);
    expect(subchannelOrdersHref("merchant", "9007199254740993")).toBe(
      "/merchant/trade/order?subchannel=9007199254740993",
    );
  });
  it("rejects another merchant and unsafe/numeric or duplicate IDs", () => {
    const other = { ...row(), merchant_id: "2" };
    expect(() => readSubchannelRow(other, "merchant", "1")).toThrow();
    expect(() =>
      readSubchannelRow({ ...row(), id: 9007199254740992 }, "merchant", "1"),
    ).toThrow();
    expect(() =>
      readSubchannelPage(
        { ...fixture.merchant, items: [row(), row()] },
        "merchant",
        "1",
      ),
    ).toThrow();
    expect(() => subchannelSessionID(9007199254740992)).toThrow();
  });
  it("does not render secret source material or a field value", () => {
    for (const key of [
      "values",
      "snapshot",
      "ciphertext",
      "nonce",
      "proof_hash",
      "account_scope",
      "private_key",
    ])
      expect(() =>
        readSubchannelRow(
          { ...row(), [key]: "synthetic-private" },
          "merchant",
          "1",
        ),
      ).toThrow();
    expect(() =>
      readSubchannelRow(
        {
          ...row(),
          fields: [
            {
              name: "secret",
              secret: true,
              present: true,
              value: "never-display",
            },
          ],
        },
        "merchant",
        "1",
      ),
    ).toThrow();
  });
  it("preserves legitimate tabs and rejects malformed state", () => {
    expect(
      readSubchannelRow({ ...row(), name: "合法\t名称" }, "merchant", "1").name,
    ).toBe("合法\t名称");
    for (const patch of [
      { weight: 0 },
      { weight: 100 },
      { enabled: "true" },
      { min_amount: "101", max_amount: "100" },
      { roll_type: 4 },
      { methods: ["native", "native"] },
    ])
      expect(() =>
        readSubchannelRow({ ...row(), ...patch }, "merchant", "1"),
      ).toThrow();
  });
  it("rejects a browser flag without actual account, parent or browser product", () => {
    for (const patch of [
      { account_available: false },
      { parent_available: false },
      { enabled: false },
      { methods: ["scan"] },
      { fee_rate_bps: null },
      { unavailable_reason: "group_unavailable" },
    ])
      expect(() =>
        readSubchannelRow(
          { ...row(), browser_test_available: true, ...patch },
          "merchant",
          "1",
        ),
      ).toThrow();
  });
  it("verifies the displayed confirmation ratio from saved integer counts", () => {
    expect(() =>
      readSubchannelRow(
        {
          ...row(),
          statistics: {
            ...row().statistics,
            attempts: "2",
            confirmed: "1",
            success_bps: 0,
          },
        },
        "merchant",
        "1",
      ),
    ).toThrow();
  });
});
describe("subchannel168b amount and route context", () => {
  it("uses exact cents at the int64 boundary", () => {
    expect(subchannelMoney("9223372036854775807")).toBe("92233720368547758.07");
    expect(subchannelMoneyInput("92233720368547758.07")).toBe(
      "9223372036854775807",
    );
    expect(subchannelMoneyInput("1.02")).toBe("102");
    expect(subchannelMoneyInput("")).toBe("0");
    for (const v of ["1.001", "1e2", "-1", "01.00", "92233720368547758.08"])
      expect(() => subchannelMoneyInput(v)).toThrow();
  });
  it("does not silently widen an invalid owner/URL context", () => {
    const filters = readSubchannelRoute(
      { uid: "9007199254740993", type: "alipay" },
      "admin",
      "168",
    );
    expect(subchannelListQuery(filters, "admin", "168")).toContain(
      "merchant_id=9007199254740993",
    );
    for (const raw of [
      { uid: "2" },
      { uid: ["1"] },
      { id: "07" },
      { type: "qqpay" },
      { uid: "1", merchant_id: "2" },
    ])
      expect(() => readSubchannelRoute(raw, "merchant", "1")).toThrow();
  });
});
