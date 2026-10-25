import { describe, it, expect } from "vitest";
import {
  canReleaseAuthorization,
  authorizationReleaseNotice,
} from "../src/authorization-release";
describe("authorization release permission and verified outcomes", () => {
  it("requires explicit server permission and disallows paid or previously sent releases", () => {
    expect(canReleaseAuthorization(true, "pending", null)).toBe(true);
    for (const flag of [false, undefined, "true"])
      expect(canReleaseAuthorization(flag, "pending", null)).toBe(false);
    expect(canReleaseAuthorization(true, "paid", null)).toBe(false);
    for (const state of ["pending", "reversal_pending"])
      expect(
        canReleaseAuthorization(true, "pending", {
          state,
          sent: false,
          request_no: "UA1",
        }),
      ).toBe(true);
    for (const state of [
      "processing",
      "unknown",
      "checking",
      "succeeded",
      "failed",
    ])
      expect(
        canReleaseAuthorization(true, "pending", {
          state,
          sent: false,
          request_no: "UA1",
        }),
      ).toBe(false);
    expect(
      canReleaseAuthorization(true, "pending", {
        state: "reversal_pending",
        sent: true,
        request_no: "UA1",
      }),
    ).toBe(false);
  });
  it("requires HTTP200 plus succeeded, keeping failed and HTTP202 unconfirmed", () => {
    expect(authorizationReleaseNotice({ state: "succeeded" }, 200)).toContain(
      "已核实买家预授权冻结已解除",
    );
    expect(authorizationReleaseNotice({ state: "succeeded" }, 200)).toContain(
      "不会增加商户余额",
    );
    expect(authorizationReleaseNotice({ state: "succeeded" }, 202)).toContain(
      "尚未核实",
    );
    expect(authorizationReleaseNotice({ state: "failed" }, 200)).toContain(
      "通道已拒绝",
    );
    expect(authorizationReleaseNotice(null, 200)).toContain("尚未核实");
  });
});
