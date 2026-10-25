import { describe, expect, it } from "vitest";
import {
  parseApplicationAuthorizationCapabilities,
  parseApplicationAuthorizationCreated,
  parseApplicationAuthorizationRequest,
  parseApplicationAuthorizationResult,
} from "../src/application-authorization";

export const appProfile = {
  id: "21",
  revision: "2",
  issuer_app_id: "2021000000000001",
  expected_auth_app_id: "2021000000000002",
  launch_kind: "basic" as const,
  app_types: [],
};
export const appRequest = (
  status = "pending",
  expires = new Date(Date.now() + 120000).toISOString(),
) => ({
  request_id: "a".repeat(64),
  purpose: "alipay_app_authorization",
  merchant_id: "1000",
  profile_id: "21",
  profile_revision: "2",
  issuer_app_id: appProfile.issuer_app_id,
  expected_auth_app_id: appProfile.expected_auth_app_id,
  launch_kind: "basic" as const,
  app_types: null,
  status,
  expires_at: expires,
});
const created = () => ({
  request: appRequest(),
  authorization_url: `https://pay.example.invalid/identity/application/authorize/${"b".repeat(64)}`,
});

describe("application authorization protocol boundaries", () => {
  it.each([
    "@",
    ":@",
    "user:@",
    ":password@",
    "user:password@",
    "%40@",
    "user%3Apassword@",
    ":pass%40word@",
  ])(
    "rejects raw URL user-info %s before capability and launch normalization",
    (userinfo) => {
      const origin = `https://${userinfo}pay.example.invalid`;
      expect
        .soft(() =>
          parseApplicationAuthorizationCapabilities(
            {
              merchant_id: "1000",
              available: true,
              reason: "",
              authorization_origin: origin,
              profiles: [appProfile],
            },
            "1000",
          ),
        )
        .toThrow();
      expect
        .soft(() =>
          parseApplicationAuthorizationCreated(
            {
              ...created(),
              authorization_url: `${origin}/identity/application/authorize/${"b".repeat(64)}`,
            },
            appProfile,
            "1000",
            "https://pay.example.invalid",
          ),
        )
        .toThrow();
      expect
        .soft(() =>
          parseApplicationAuthorizationCreated(
            created(),
            appProfile,
            "1000",
            origin,
          ),
        )
        .toThrow();
      const assigned = {
        ...appProfile,
        launch_kind: "assigned" as const,
        app_types: ["WEBAPP"],
      };
      expect
        .soft(() =>
          parseApplicationAuthorizationCreated(
            {
              ...created(),
              request: {
                ...appRequest(),
                launch_kind: "assigned",
                app_types: ["WEBAPP"],
              },
              wallet_authorization_url: `${origin}/identity/application/wallet/${"b".repeat(64)}`,
            },
            assigned,
            "1000",
            "https://pay.example.invalid",
          ),
        )
        .toThrow();
    },
  );
  it("accepts the separate metadata and basic/assigned URL contracts without a user identity or payment", () => {
    const caps = parseApplicationAuthorizationCapabilities(
      {
        merchant_id: "1000",
        available: true,
        reason: "",
        authorization_origin: "https://pay.example.invalid",
        profiles: [appProfile],
      },
      "1000",
    );
    expect(caps.profiles[0]).toEqual(appProfile);
    const q = parseApplicationAuthorizationRequest(
      appRequest(),
      appProfile,
      "1000",
    );
    expect(q.purpose).toBe("alipay_app_authorization");
    expect(q.app_types).toEqual([]);
    const basic = parseApplicationAuthorizationCreated(
      created(),
      appProfile,
      "1000",
      "https://pay.example.invalid",
    );
    expect(basic.browserURL).toContain("/identity/application/authorize/");
    const p = {
      ...appProfile,
      launch_kind: "assigned" as const,
      app_types: ["WEBAPP", "TINYAPP"],
    };
    const assigned = parseApplicationAuthorizationCreated(
      {
        ...created(),
        request: {
          ...appRequest(),
          launch_kind: "assigned",
          app_types: ["WEBAPP", "TINYAPP"],
        },
        wallet_authorization_url: `https://pay.example.invalid/identity/application/wallet/${"b".repeat(64)}`,
      },
      p,
      "1000",
      "https://pay.example.invalid",
    );
    expect(assigned.walletURL).toContain("/identity/application/wallet/");
  });
  it("rejects capability ID ambiguity, private fields and invalid profile launch types", () => {
    const caps = (profile: unknown) => ({
      merchant_id: "1000",
      available: true,
      reason: "",
      authorization_origin: "https://pay.example.invalid",
      profiles: [profile],
    });
    for (const p of [
      { ...appProfile, id: 21 },
      { ...appProfile, revision: "02" },
      { ...appProfile, issuer_app_id: "user-app" },
      { ...appProfile, expected_auth_app_id: "" },
      { ...appProfile, launch_kind: "oauth" },
      { ...appProfile, app_types: ["WEBAPP"] },
      { ...appProfile, launch_kind: "assigned", app_types: [] },
      {
        ...appProfile,
        launch_kind: "assigned",
        app_types: ["WEBAPP", "WEBAPP"],
      },
      { ...appProfile, launch_kind: "assigned", app_types: ["unknown"] },
      { ...appProfile, config: {} },
      { ...appProfile, app_auth_token: "private" },
      { ...appProfile, merchant_ids: ["1000"] },
    ])
      expect(() =>
        parseApplicationAuthorizationCapabilities(caps(p), "1000"),
      ).toThrow();
    expect(() =>
      parseApplicationAuthorizationCapabilities(
        { ...caps(appProfile), merchant_id: "1001" },
        "1000",
      ),
    ).toThrow();
    expect(() =>
      parseApplicationAuthorizationCapabilities(
        { ...caps(appProfile), available: false },
        "1000",
      ),
    ).toThrow();
    expect(() =>
      parseApplicationAuthorizationCapabilities(
        { ...caps(appProfile), profiles: [appProfile, appProfile] },
        "1000",
      ),
    ).toThrow();
    for (const origin of [
      "",
      "http://pay.example.invalid",
      "https://user@pay.example.invalid",
      "https://pay.example.invalid/path",
      "https://pay.example.invalid?",
      "https://pay.example.invalid#",
    ])
      expect(() =>
        parseApplicationAuthorizationCapabilities(
          { ...caps(appProfile), authorization_origin: origin },
          "1000",
        ),
      ).toThrow();
  });
  it("rejects cross-purpose, application/merchant/version/request/expiry changes and secret-bearing metadata", () => {
    for (const changed of [
      { purpose: "web_identity" },
      { merchant_id: "1001" },
      { profile_id: "22" },
      { profile_revision: "3" },
      { issuer_app_id: "2021000000000003" },
      { expected_auth_app_id: "2021000000000004" },
      { launch_kind: "assigned" },
      { app_auth_token: "must-not-display" },
      { user_id: "2088000000000000" },
      { subject_id: "wrong-purpose" },
      { status: "paid" },
      { expires_at: new Date(Date.now() + 600000).toISOString() },
    ])
      expect(() =>
        parseApplicationAuthorizationRequest(
          { ...appRequest(), ...changed },
          appProfile,
          "1000",
        ),
      ).toThrow();
    const previous = parseApplicationAuthorizationRequest(
      appRequest(),
      appProfile,
      "1000",
    );
    expect(() =>
      parseApplicationAuthorizationRequest(
        { ...appRequest(), request_id: "c".repeat(64) },
        appProfile,
        "1000",
        previous,
      ),
    ).toThrow();
    expect(() =>
      parseApplicationAuthorizationRequest(
        {
          ...appRequest(),
          expires_at: new Date(
            Date.parse(previous.expires_at) + 1000,
          ).toISOString(),
        },
        appProfile,
        "1000",
        previous,
      ),
    ).toThrow();
  });
  it("rejects arbitrary gateways, user OAuth paths, unsafe URLs and mismatched assigned launch tokens", () => {
    for (const url of [
      `http://pay.example.invalid/identity/application/authorize/${"b".repeat(64)}`,
      `https://pay.example.invalid/identity/authorize/${"b".repeat(64)}`,
      `https://openauth.alipay.com/identity/application/authorize/${"b".repeat(64)}`,
      `https://pay.example.invalid/identity/application/authorize/${"b".repeat(64)}?code=secret`,
      `https://pay.example.invalid/identity/application/authorize/${"b".repeat(64)}#`,
      `https://user@pay.example.invalid/identity/application/authorize/${"b".repeat(64)}`,
    ])
      expect(() =>
        parseApplicationAuthorizationCreated(
          { ...created(), authorization_url: url },
          appProfile,
          "1000",
          "https://pay.example.invalid",
        ),
      ).toThrow();
    expect(() =>
      parseApplicationAuthorizationCreated(
        {
          ...created(),
          wallet_authorization_url: "https://pay.example.invalid/wallet",
        },
        appProfile,
        "1000",
        "https://pay.example.invalid",
      ),
    ).toThrow();
    const p = {
      ...appProfile,
      launch_kind: "assigned" as const,
      app_types: ["WEBAPP"],
    };
    expect(() =>
      parseApplicationAuthorizationCreated(
        {
          ...created(),
          request: {
            ...appRequest(),
            launch_kind: "assigned",
            app_types: ["WEBAPP"],
          },
          wallet_authorization_url: `https://pay.example.invalid/identity/application/wallet/${"c".repeat(64)}`,
        },
        p,
        "1000",
        "https://pay.example.invalid",
      ),
    ).toThrow();
  });
  it("validates app token separately, binds issuer/authorized application and preserves unknown token lifetime", () => {
    const q = parseApplicationAuthorizationRequest(
      appRequest("verified"),
      appProfile,
      "1000",
    );
    const result = {
      provider: "alipay",
      issuer_app_id: appProfile.issuer_app_id,
      auth_app_id: appProfile.expected_auth_app_id,
      user_id: "2088000000000000",
      app_auth_token: "synthetic-app-token",
      token_expires_in_seconds: "0",
    };
    expect(
      parseApplicationAuthorizationResult(result, q).token_expires_in_seconds,
    ).toBe("0");
    for (const changed of [
      { issuer_app_id: appProfile.expected_auth_app_id },
      { auth_app_id: appProfile.issuer_app_id },
      { provider: "wxpay" },
      { user_id: "subject" },
      { app_auth_token: "@excluded" },
      { app_auth_token: "bad\ntoken" },
      { app_auth_token: "x".repeat(4097) },
      { token_expires_in_seconds: 0 },
      { token_expires_in_seconds: "01" },
      { token_expires_in_seconds: "-1" },
      { app_refresh_token: "discard" },
      { subject_id: "wrong-purpose" },
    ])
      expect(() =>
        parseApplicationAuthorizationResult({ ...result, ...changed }, q),
      ).toThrow();
  });
});
