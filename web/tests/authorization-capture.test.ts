import { describe, it, expect } from "vitest";
import { canCaptureAuthorization as can } from "../src/authorization-release";
describe("capture is explicit and mutually exclusive with prior consumption or release", () => {
  it("only permits pending untouched authorization with server permission", () => {
    expect(
      can(true, "pending", { capture_state: "none", release_state: "none" }),
    ).toBe(true);
    for (const state of ["unknown", "processing", "succeeded", "failed"])
      expect(
        can(true, "pending", { capture_state: state, release_state: "none" }),
      ).toBe(false);
    for (const state of [
      "unknown",
      "checking",
      "succeeded",
      "reversal_pending",
    ])
      expect(
        can(true, "pending", { capture_state: "none", release_state: state }),
      ).toBe(false);
    expect(
      can(false, "pending", { capture_state: "none", release_state: "none" }),
    ).toBe(false);
    expect(
      can(true, "paid", { capture_state: "none", release_state: "none" }),
    ).toBe(false);
    expect(can(true, "pending", {})).toBe(false);
  });
});
