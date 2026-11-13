import { expect, it } from "vitest";
import {
  readCryptoChannel,
  readPaymentChannelList,
  readCryptoChannelOptions,
  cryptoChannelInput,
} from "../src/crypto-channels";

export const cryptoChannelFixture = {
  asset: "USDT",
  id: "9007199254740993",
  name: "TRON 收款",
  pay_type: "usdt",
  plugin: "usdt_tron",
  configured: true,
  network: "tron",
  revision: "3",
  enabled: true,
  archived: false,
  runtime_enabled: false,
  fee_bps: 125,
  min_amount: "0.1",
  max_amount: "1000",
  day_limit: "2000",
  created_at: "2026-11-13T00:00:00Z",
  updated_at: "2026-11-13T00:00:00Z",
};
it("reads exact USDT identities without pretending to be a CNY parent account", () => {
  expect(readCryptoChannel(cryptoChannelFixture).id).toBe("9007199254740993");
  const list = readPaymentChannelList({
    items: [cryptoChannelFixture],
    total: 1,
    page: 1,
    size: 20,
  });
  expect(list.items[0]).toMatchObject({
    asset: "USDT",
    runtime_enabled: false,
  });
  for (const patch of [
    { id: 9007199254740993 },
    { network: "bitcoin" },
    { plugin: "usdt_ethereum" },
    { fee_bps: 1.2 },
    { min_amount: "0.0000001" },
    { merchant_ids: ["123"] },
    { private_key: "secret" },
  ]) {
    expect(() =>
      readCryptoChannel({ ...cryptoChannelFixture, ...patch }),
    ).toThrow();
  }
});
it("rejects malformed mixed catalog instead of hiding duplicate identities", () => {
  expect(() =>
    readPaymentChannelList({
      items: [cryptoChannelFixture, cryptoChannelFixture],
      total: 2,
      page: 1,
      size: 20,
    }),
  ).toThrow();
  expect(() =>
    readPaymentChannelList({
      items: [cryptoChannelFixture],
      total: 1,
      page: 0,
      size: 20,
    }),
  ).toThrow();
});
it("accepts only safe merchant channel choices without admin grants", () => {
  const option = {
    id: "42",
    name: "Ethereum",
    network: "ethereum",
    fee_bps: 70,
    min_amount: "0",
    max_amount: "0",
    day_limit: "0",
  };
  expect(readCryptoChannelOptions({ items: [option] })).toEqual([option]);
  expect(() => readCryptoChannelOptions([])).toThrow();
  expect(() =>
    readCryptoChannelOptions({ items: [{ ...option, merchant_ids: ["123"] }] }),
  ).toThrow();
});
it("validates full channel edit and preserves explicit disabled state and micro amounts", () => {
  const draft = {
    name: "TRON",
    network: "tron",
    enabled: false,
    archived: false,
    fee: "1.25",
    min_amount: "0.000001",
    max_amount: "100",
    day_limit: "0",
  };
  expect(cryptoChannelInput(draft, "3")).toMatchObject({
    expected_revision: "3",
    fee_bps: 125,
    enabled: false,
    min_amount: "0.000001",
  });
  expect(cryptoChannelInput(draft, "3")).not.toHaveProperty("merchant_ids");
  expect(() =>
    cryptoChannelInput({ ...draft, min_amount: "101" }, "3"),
  ).toThrow();
  expect(() => cryptoChannelInput({ ...draft, fee: "1.234" }, "3")).toThrow();
});
it("normalizes decimal zero limits without rounding nonzero USDT amounts", () => {
  const draft = {
    name: "TRON",
    network: "tron",
    enabled: false,
    archived: false,
    fee: "0",
    min_amount: "0.000000",
    max_amount: "0.00",
    day_limit: "0.0",
  };
  expect(cryptoChannelInput(draft, "0")).toMatchObject({
    min_amount: "0",
    max_amount: "0",
    day_limit: "0",
  });
  expect(
    cryptoChannelInput(
      {
        ...draft,
        min_amount: "0.000001",
        max_amount: "8999999999.999999",
        day_limit: "9000000000",
      },
      "0",
    ),
  ).toMatchObject({
    min_amount: "0.000001",
    max_amount: "8999999999.999999",
    day_limit: "9000000000",
  });
});
