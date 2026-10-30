import { expect, it } from "vitest";
import * as enrollment from "../src/enrollment";
import {
  enrollmentError,
  parseEnrollmentCapabilities,
} from "../src/enrollment";
it("fails closed on missing public capability fields and retains explicit mail-disabled state", () => {
  expect(() =>
    parseEnrollmentCapabilities({ registration_open: true }),
  ).toThrow();
  expect(() =>
    parseEnrollmentCapabilities({
      registration_open: true,
      email_enabled: "false",
      invite_required: false,
      expires_seconds: 600,
    }),
  ).toThrow();
  expect(
    parseEnrollmentCapabilities({
      registration_open: false,
      email_enabled: false,
      invite_required: true,
      expires_seconds: 600,
    }).email_enabled,
  ).toBe(false);
});
it("explains rate limits without inventing retry timing or successful delivery", () => {
  expect(enrollmentError(429, "limited")).toContain("稍后");
  expect(enrollmentError(503, "mail missing")).toContain("未确认发送");
  expect(enrollmentError(422, "invalid code")).toContain("验证码");
});
it("strictly reads the original review capability and defaults old responses to false", () => {
  const base = {
    registration_open: true,
    email_enabled: true,
    invite_required: false,
    expires_seconds: 600,
  };
  expect(() =>
    parseEnrollmentCapabilities({ ...base, review_required: "true" }),
  ).toThrow();
  expect(
    parseEnrollmentCapabilities({ ...base, review_required: true })
      .review_required,
  ).toBe(true);
  expect(parseEnrollmentCapabilities(base).review_required).toBe(false);
});
it("reads the SMS capability strictly and defaults absent responses to false", () => {
  const base = {
    registration_open: true,
    email_enabled: true,
    invite_required: false,
    expires_seconds: 600,
  };
  expect(parseEnrollmentCapabilities(base).sms_enabled).toBe(false);
  expect(
    parseEnrollmentCapabilities({ ...base, sms_enabled: true }).sms_enabled,
  ).toBe(true);
  expect(() =>
    parseEnrollmentCapabilities({ ...base, sms_enabled: "true" }),
  ).toThrow();
});
it("reads the verify_type capability strictly and defaults absent responses to empty", () => {
  const base = {
    registration_open: true,
    email_enabled: true,
    invite_required: false,
    expires_seconds: 600,
  };
  expect(parseEnrollmentCapabilities(base).verify_type).toBe("");
  expect(
    parseEnrollmentCapabilities({ ...base, verify_type: "email" }).verify_type,
  ).toBe("email");
  expect(
    parseEnrollmentCapabilities({ ...base, verify_type: "phone" }).verify_type,
  ).toBe("phone");
  expect(() =>
    parseEnrollmentCapabilities({ ...base, verify_type: "sms" }),
  ).toThrow();
});
it("pending pay2 registration tells the user to wait for review and rejects malformed result states", () => {
  expect("registrationNotice" in enrollment).toBe(true);
  const notice = (
    enrollment as unknown as { registrationNotice: (x: unknown) => string }
  ).registrationNotice;
  expect(
    notice({ merchant_id: "82", username: "merchant82", pay: "2" }),
  ).toContain("待审核");
  expect(
    notice({ merchant_id: "82", username: "merchant82", pay: "0" }),
  ).toContain("禁止收款");
  expect(
    notice({ merchant_id: "82", username: "merchant82", pay: "1" }),
  ).toContain("注册完成");
  expect(notice({ merchant_id: "82", username: "merchant82" })).toContain(
    "注册完成",
  );
  for (const pay of [2, "3", null, false])
    expect(() =>
      notice({ merchant_id: "82", username: "merchant82", pay }),
    ).toThrow();
});
