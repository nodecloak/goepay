import { describe, expect, it } from "vitest";
import {
  parsePersonalSettings,
  personalSettingsBody,
} from "../src/merchant-personal-settings";

const merchantID = "9007199254740993";
const state = {
  merchant_id: merchantID,
  qq: "123456",
  pay_minmoney: "1.01",
  pay_maxmoney: "2.00",
  remain_money: "10%",
  bounds_valid: true,
  fingerprint: "a".repeat(64),
  can_update: true as const,
};
describe("original merchant personal QQ and limits", () => {
  it("retains the full own ID and exact minor amounts", () => {
    expect(parsePersonalSettings(state, merchantID)).toEqual(state);
    expect(
      parsePersonalSettings(
        { ...state, pay_maxmoney: "92233720368547758.07" },
        merchantID,
      ).pay_maxmoney,
    ).toBe("92233720368547758.07");
  });
  it("rejects foreign, numeric, unsafe and non-exact DTO fields", () => {
    for (const bad of [
      { ...state, merchant_id: "9007199254740995" },
      { ...state, merchant_id: 9007199254740993 },
      { ...state, merchant_id: "9223372036854775808" },
      { ...state, api_key: "secret" },
      { ...state, can_update: false },
      { ...state, fingerprint: "x".repeat(64) },
      { ...state, qq: "1234" },
      { ...state, remain_money: "100%" },
      { ...state, remain_money: "abc" },
    ])
      expect(() => parsePersonalSettings(bad, merchantID)).toThrow();
  });
  it("requires canonical safe response money and truthful range validity", () => {
    for (const bad of [
      { ...state, pay_minmoney: "1" },
      { ...state, pay_minmoney: "1e1" },
      { ...state, pay_minmoney: "-1.00" },
      { ...state, pay_maxmoney: "92233720368547758.08" },
      { ...state, bounds_valid: false },
    ])
      expect(() => parsePersonalSettings(bad, merchantID)).toThrow();
    expect(
      parsePersonalSettings(
        { ...state, pay_minmoney: "3.00", bounds_valid: false },
        merchantID,
      ).bounds_valid,
    ).toBe(false);
  });
  it("sends only original three editable fields and original fingerprint", () => {
    expect(
      personalSettingsBody(state, {
        qq: "7654321",
        pay_minmoney: "1.1",
        pay_maxmoney: "",
        remain_money: "50.00",
      }),
    ).toEqual({
      qq: "7654321",
      pay_minmoney: "1.10",
      pay_maxmoney: "",
      remain_money: "50.00",
      expected_fingerprint: state.fingerprint,
    });
  });
  it("preserves blank and explicit zero without granting a group override", () => {
    expect(
      personalSettingsBody(state, {
        qq: "",
        pay_minmoney: "",
        pay_maxmoney: "0",
        remain_money: "",
      }),
    ).toEqual({
      qq: "",
      pay_minmoney: "",
      pay_maxmoney: "0.00",
      remain_money: "",
      expected_fingerprint: state.fingerprint,
    });
  });
  it("rejects contradictory intervals, exponents, negative, precision and overflow", () => {
    for (const draft of [
      { qq: "1234", pay_minmoney: "", pay_maxmoney: "", remain_money: "" },
      {
        qq: "123456",
        pay_minmoney: "3",
        pay_maxmoney: "2",
        remain_money: "",
      },
      {
        qq: "123456",
        pay_minmoney: "",
        pay_maxmoney: "",
        remain_money: "100%",
      },
      { qq: "123456", pay_minmoney: "", pay_maxmoney: "", remain_money: "x" },
      ...["-1", "1e2", "1.001", "01.00", "92233720368547758.08"].map(
        (pay_minmoney) => ({
          qq: "123456",
          pay_minmoney,
          pay_maxmoney: "",
          remain_money: "",
        }),
      ),
    ])
      expect(() => personalSettingsBody(state, draft)).toThrow();
  });
});
