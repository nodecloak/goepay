import { expect, it } from "vitest";
import {
  parseSystemUpdate183,
  parseUpdateStatus183,
} from "../src/system-update183";
import actual from "../e2e/systemupdate183-http-fixture.json";

it("reads disposed actual Go HTTP version fields and real preparation progress", () => {
  expect(actual.schema_disposed).toBe(true);
  expect(actual.session_invalid_after_cleanup).toBe(true);
  expect(parseSystemUpdate183(actual.available).latest_version_name).toBe(
    "合成离线版本183",
  );
  for (const value of [
    actual.accepted,
    actual.prepared,
    actual.applied,
    actual.unknown,
    actual.cancelled,
  ])
    expect(parseUpdateStatus183(value).intent).toBe(
      actual.available.fingerprint,
    );
  expect(parseUpdateStatus183(actual.prepared).applied).toBe(false);
  expect(parseUpdateStatus183(actual.applied).applied).toBe(true);
  expect(
    parseUpdateStatus183(actual.long_version).target!.version,
  ).toHaveLength(76);
});
it("rejects unbound identities, invented completion, unknown fields and impossible progress", () => {
  for (const value of [
    { ...actual.prepared, intent: "different" },
    { ...actual.prepared, applied: true },
    {
      ...actual.prepared,
      target: { ...actual.prepared.target, fingerprint: "b".repeat(64) },
    },
    {
      ...actual.prepared,
      progress: {
        ...actual.prepared.progress,
        read_bytes: actual.prepared.progress.total_bytes + 1,
      },
    },
    { ...actual.prepared, extra: "private_path" },
  ])
    expect(() => parseUpdateStatus183(value)).toThrow();
  expect(() =>
    parseSystemUpdate183({ ...actual.available, can_prepare: "true" }),
  ).toThrow();
});
