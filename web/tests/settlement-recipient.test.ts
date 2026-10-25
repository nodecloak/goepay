import { expect, it } from "vitest";
import {
  readSettlementRecipient,
  recipientConfigurationBody,
  settlementSnapshotLabel,
} from "../src/settlement-recipient";
it("accepts only masked versioned recipient metadata and builds explicit CAS edits", () => {
  const v = readSettlementRecipient({
    state: "configured",
    config_id: "42",
    type: "alipay",
    app_id: "",
    account_masked: "***1234",
    name_masked: "***",
  });
  expect(v.config_id).toBe("42");
  expect(
    recipientConfigurationBody(
      {
        type: "alipay",
        account: " private-account ",
        name: " Name ",
        bank_code: "",
        app_id: "",
      },
      v,
      "101",
    ),
  ).toEqual({
    type: "alipay",
    account: "private-account",
    name: "Name",
    bank_code: "",
    app_id: "",
    expected_config_id: "42",
    merchant_id: "101",
  });
  expect(
    readSettlementRecipient({
      state: "missing",
      config_id: "0",
      type: "",
      app_id: "",
      account_masked: "",
      name_masked: "",
    }).state,
  ).toBe("missing");
  for (const x of [
    null,
    {},
    { ...v, config_id: 42 },
    { ...v, config_id: "0" },
    { ...v, account: "plaintext" },
    { ...v, name: "plaintext" },
    { ...v, account_masked: "private-account" },
  ])
    expect(() => readSettlementRecipient(x)).toThrow();
  expect(() =>
    recipientConfigurationBody(
      {
        type: "wxpay",
        account: "openid",
        name: "Name",
        bank_code: "",
        app_id: "",
      },
      v,
    ),
  ).toThrow();
  expect(settlementSnapshotLabel(null)).toContain("不能出款");
  expect(settlementSnapshotLabel({ ...v, state: "recorded" })).toContain("42");
});
