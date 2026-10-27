import { it, expect } from "vitest";
import {
  readSignaturePolicy,
  readSignaturePolicyResult,
  signaturePolicyLabel,
  signaturePolicyPath,
} from "../src/merchant-signature-policy";

it("binds administrator policy requests to a canonical full merchant ID", () => {
  expect(signaturePolicyPath("merchant")).toBe("/account/signature-policy");
  expect(signaturePolicyPath("admin", "9007199254740993")).toBe(
    "/merchants/9007199254740993/signature-policy",
  );
  for (const value of [
    undefined,
    "",
    "01",
    "0",
    "-1",
    "9223372036854775808",
    "1?pid=2",
  ])
    expect(() => signaturePolicyPath("admin", value)).toThrow();
  expect(() => signaturePolicyPath("merchant", "1")).toThrow();
});
const state = {
  merchant_id: "9007199254740993",
  keytype: null,
  algorithm: "MD5",
  rsa_configured: true,
  fingerprint: "a".repeat(64),
  can_update: true,
};
it("parses exact full-ID policy and rejects untrusted algorithm, capability and secret fields", () => {
  expect(readSignaturePolicy(state)).toEqual(state);
  for (const v of [
    null,
    [],
    { ...state, merchant_id: 9007199254740993 },
    { ...state, merchant_id: "01" },
    { ...state, merchant_id: "9223372036854775808" },
    { ...state, keytype: "0" },
    { ...state, keytype: 2 },
    { ...state, algorithm: "BOTH" },
    { ...state, rsa_configured: "true" },
    { ...state, can_update: 1 },
    { ...state, fingerprint: "A".repeat(64) },
    { ...state, api_key: "secret" },
    { ...state, private_key: "secret" },
    { ...state, keytype: 1, rsa_configured: false },
  ])
    expect(() => readSignaturePolicy(v)).toThrow();
});
it("policy result must match original owner, chosen mode and fresh fingerprint without changing credential properties", () => {
  const before = readSignaturePolicy(state),
    after = { ...state, keytype: 1, fingerprint: "b".repeat(64) };
  expect(readSignaturePolicyResult(after, before, 1)).toEqual(after);
  for (const v of [
    { ...after, merchant_id: "9007199254740995" },
    { ...after, keytype: 0 },
    { ...after, algorithm: "RSA" },
    { ...after, fingerprint: state.fingerprint },
    { ...after, rsa_configured: false },
    { ...after, can_update: false },
  ])
    expect(() => readSignaturePolicyResult(v, before, 1)).toThrow();
});
it("names original compatible and RSA-only modes and clearly identifies unchanged legacy policy", () => {
  expect(
    signaturePolicyLabel(readSignaturePolicy({ ...state, keytype: 0 })),
  ).toBe("MD5+RSA兼容");
  expect(
    signaturePolicyLabel(readSignaturePolicy({ ...state, keytype: 1 })),
  ).toBe("仅RSA");
  expect(signaturePolicyLabel(readSignaturePolicy(state))).toBe(
    "仅MD5（历史配置）",
  );
  expect(
    signaturePolicyLabel(readSignaturePolicy({ ...state, algorithm: "RSA" })),
  ).toBe("仅RSA（历史配置）");
});
