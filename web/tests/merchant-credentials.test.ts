import { it, expect } from "vitest";
import {
  readMerchantCredentialState,
  readMerchantCredentialResult,
} from "../src/merchant-credentials";
const id = "9223372036854775807",
  state = {
    merchant_id: id,
    revision: "1",
    algorithm: "MD5",
    public_key: "",
    md5_configured: true,
    fingerprint: "a".repeat(64),
  };
const key = "a".repeat(32),
  md5 = {
    merchant_id: id,
    revision: "2",
    algorithm: "MD5",
    fingerprint: "b".repeat(64),
    api_key: key,
  };
it("binds credential state to exact merchant IDs and rejects secret-bearing reads", () => {
  expect(readMerchantCredentialState(state, id)).toEqual(state);
  for (const raw of [
    null,
    [],
    { ...state, merchant_id: Number(id) },
    { ...state, merchant_id: "1" },
    { ...state, revision: 1 },
    { ...state, revision: "01" },
    { ...state, revision: "9223372036854775808" },
    { ...state, algorithm: "BOTH" },
    { ...state, md5_configured: "true" },
    { ...state, fingerprint: "A".repeat(64) },
    { ...state, api_key: "old-secret" },
    { ...state, private_key: "old-private" },
  ])
    expect(() => readMerchantCredentialState(raw, id)).toThrow();
});
it("accepts one committed revision and operation-specific one-time material", () => {
  const before = readMerchantCredentialState(state, id);
  expect(readMerchantCredentialResult(md5, before, "reset-md5")).toMatchObject({
    state: { merchant_id: id, revision: "2", fingerprint: "b".repeat(64) },
    apiKey: key,
  });
  for (const raw of [
    { ...md5, merchant_id: "1" },
    { ...md5, revision: "3" },
    { ...md5, revision: 2 },
    { ...md5, algorithm: "RSA" },
    { ...md5, fingerprint: state.fingerprint },
    { ...md5, api_key: "A".repeat(32) },
    { ...md5, private_key: "unexpected" },
  ])
    expect(() =>
      readMerchantCredentialResult(raw, before, "reset-md5"),
    ).toThrow();
  const publicKey =
      "-----BEGIN PUBLIC KEY-----\nU1lOVEhFVElD\n-----END PUBLIC KEY-----\n",
    privateKey =
      "-----BEGIN PRIVATE KEY-----\nU1lOVEhFVElD\n-----END PRIVATE KEY-----\n";
  const rsa = {
    merchant_id: id,
    revision: "2",
    algorithm: "MD5",
    fingerprint: "b".repeat(64),
    public_key: publicKey,
    private_key: privateKey,
  };
  expect(
    readMerchantCredentialResult(rsa, before, "generate-rsa"),
  ).toMatchObject({
    state: { public_key: publicKey, algorithm: "MD5" },
    privateKey,
  });
  expect(() =>
    readMerchantCredentialResult(
      { ...rsa, private_key: "not-a-key" },
      before,
      "generate-rsa",
    ),
  ).toThrow();
  expect(() =>
    readMerchantCredentialResult(rsa, before, "reset-md5"),
  ).toThrow();
});
