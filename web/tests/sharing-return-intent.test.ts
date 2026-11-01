import { afterEach, expect, it } from "vitest";
import {
  SharingReturnIntentStore,
  type SharingReturnIntent,
} from "../src/sharing-return-intent";
const make = () =>
  new SharingReturnIntentStore(
    "a".repeat(64),
    "751",
    "101",
    "original-sharing",
  );
const intent = (): SharingReturnIntent => ({
  version: 1,
  merchant_id: "751",
  sharing_id: "101",
  sharing_external_no: "original-sharing",
  receiver_id: "9",
  external_no: "return_" + "a".repeat(24),
  amount: "10",
  state: "unknown",
});
afterEach(() => localStorage.clear());
it("does not replace another tab's unresolved request with a new receiver or request number", () => {
  const a = make(),
    b = make();
  const original = a.save(intent());
  expect(() =>
    b.save({ ...intent(), external_no: "return_" + "b".repeat(24) }),
  ).toThrow();
  expect(a.read()).toEqual(original);
});
it("does not clear an accepted result using an older unknown snapshot", () => {
  const a = make(),
    b = make();
  const unknown = a.save(intent());
  b.save({ ...unknown, state: "succeeded", record_id: "301" });
  expect(() => a.clear(unknown)).toThrow();
  expect(a.read()?.state).toBe("succeeded");
});
it("a stale observation cannot regress a verified terminal result", () => {
  const a = make(),
    old = a.save(intent());
  const final = a.save({ ...old, state: "succeeded", record_id: "301" });
  expect(a.save({ ...old, state: "pending", record_id: "301" })).toEqual(final);
});
it("persists only canonical original-request data and refuses foreign or sensitive fields", () => {
  const a = make();
  a.save(intent());
  expect(make().read()).toEqual(intent());
  for (const value of [
    { ...intent(), merchant_id: "752" },
    { ...intent(), amount: "01" },
    { ...intent(), authorization_token: "forbidden" },
  ]) {
    localStorage.setItem(a.key, JSON.stringify(value));
    expect(() => a.read()).toThrow();
  }
});
