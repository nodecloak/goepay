import { it, expect } from "vitest";
import {
  parseIdentityProfile,
  parseProfileRequest,
  parseProfileLaunch,
  parseProfileIdentityLink,
} from "../src/identity-profile";
const p = {
  id: "11",
  revision: "1",
  channel_id: "7",
  provider: "wxpay",
  kind: "mini",
  app_id: "independent-app",
  path: "pages/identity/index",
};
const q = {
  profile_id: "11",
  profile_revision: "1",
  channel_id: "7",
  merchant_id: "1",
  provider: "wxpay",
  kind: "mini",
  app_id: p.app_id,
  path: p.path,
  request_id: "a".repeat(64),
  status: "pending",
  expires_at: new Date(Date.now() + 240000).toISOString(),
};
it("requires exact trusted application namespace and fixed independent mini callback without payment data", () => {
  const profile = parseIdentityProfile(p);
  const request = parseProfileRequest(q, profile, "1");
  const launch = {
    kind: "sdk",
    provider: p.provider,
    app_id: p.app_id,
    path: p.path,
    method: "POST",
    completion_url: `https://pay.example.invalid/identity/profile/mini/wxpay/11/${q.request_id}/`,
    body: { state: "b".repeat(64) },
    expires_at: q.expires_at,
  };
  expect(parseProfileLaunch(launch, request).body.state).toBe("b".repeat(64));
  for (const bad of [
    { ...q, app_id: "payment-app" },
    { ...q, profile_revision: "2" },
    { ...q, subject_id: "secret" },
  ])
    expect(() => parseProfileRequest(bad, profile, "1")).toThrow();
  for (const bad of [
    { ...launch, kind: "wxapp" },
    { ...launch, body: { state: "b".repeat(64), openid: "forged" } },
    {
      ...launch,
      completion_url:
        "https://pay.example.invalid/oauth/mini/complete/wxpay/7/order/?state=x",
    },
  ])
    expect(() => parseProfileLaunch(bad, request)).toThrow();
});

it("accepts URL Link only for explicitly enabled WeChat mini profiles", () => {
  expect(parseIdentityProfile(p).url_link_available).toBe(false);
  expect(
    parseIdentityProfile({ ...p, url_link_available: true }).url_link_available,
  ).toBe(true);
  for (const bad of [null, "true", 1])
    expect(() =>
      parseIdentityProfile({ ...p, url_link_available: bad }),
    ).toThrow();
  expect(() =>
    parseIdentityProfile({
      ...p,
      kind: "web",
      path: "",
      url_link_available: true,
    }),
  ).toThrow();
  expect(() =>
    parseIdentityProfile({
      ...p,
      provider: "alipay",
      url_link_available: true,
    }),
  ).toThrow();
});

it("binds a stored URL Link to the original identity request and requested cutoff without claiming provider expiry", () => {
  const request = parseProfileRequest(q, parseIdentityProfile(p), "1");
  const link = {
    purpose: "wechat_mini_identity_url_link",
    request_id: q.request_id,
    provider: "wxpay",
    app_id: p.app_id,
    status: "ready",
    created_at: new Date(Date.now() - 1000).toISOString(),
    expires_at: q.expires_at,
    requested_expires_at: new Date(
      Math.floor(Date.parse(q.expires_at) / 1000) * 1000,
    ).toISOString(),
    url_link: "https://wxaurl.cn/synthetic-ticket",
  };
  expect(parseProfileIdentityLink(link, request).url_link).toBe(link.url_link);
  expect(
    parseProfileIdentityLink(
      {
        ...link,
        url_link:
          "https://wxmpurl.cn/synthetic-ticket?cq=opaque%3D1%26part%3D2",
      },
      request,
    ).status,
  ).toBe("ready");
  for (const status of ["creating", "failed", "unknown"]) {
    const { url_link, ...metadata } = link;
    expect(
      parseProfileIdentityLink({ ...metadata, status }, request).status,
    ).toBe(status);
  }
  for (const changed of [
    { purpose: "mini_payment" },
    { request_id: "c".repeat(64) },
    { provider: "alipay" },
    { app_id: "payment-app" },
    { status: "pending" },
    { expires_at: new Date(Date.parse(q.expires_at) + 1000).toISOString() },
    {
      requested_expires_at: new Date(
        Math.floor(Date.parse(q.expires_at) / 1000) * 1000 + 1000,
      ).toISOString(),
    },
    { created_at: new Date(Date.now() + 60000).toISOString() },
    { created_at: new Date(Date.parse(q.expires_at) - 301000).toISOString() },
    { access_token: "must-not-display" },
    { state: "must-not-echo" },
    { status: "unknown" },
  ])
    expect(() =>
      parseProfileIdentityLink({ ...link, ...changed }, request),
    ).toThrow();
  const previous = parseProfileIdentityLink(link, request);
  expect(() =>
    parseProfileIdentityLink(
      {
        ...link,
        created_at: new Date(Date.parse(link.created_at) + 1000).toISOString(),
      },
      request,
      previous,
    ),
  ).toThrow();
  for (const url_link of [
    "https://@wxaurl.cn/ticket",
    "https://:@wxaurl.cn/ticket",
    "https://user:password@wxaurl.cn/ticket",
    "https://wxaurl.cn:443/ticket",
    "https://sub.wxaurl.cn/ticket",
    "https://evil.example.invalid/ticket",
    "http://wxaurl.cn/ticket",
    "https://wxaurl.cn/",
    "https://wxaurl.cn//",
    "https://wxaurl.cn/ticket#",
    "https://wxaurl.cn/ticket?",
    "https://wxaurl.cn/a/../ticket",
    "https://wxaurl.cn/%2e%2e/ticket",
    "https://wxaurl.cn/%0a",
    "https://wxaurl.cn/ticket\n",
    "https://wxaurl.cn/ticket?access_token=secret",
    "https://wxaurl.cn/ticket?cq=one&cq=two",
    "https://wxaurl.cn/ticket?cq=%0a",
    "https://wxaurl.cn/ticket?cq=%",
    "https://wxaurl.cn/ticket?cq=" + "a".repeat(257),
  ])
    expect(() =>
      parseProfileIdentityLink({ ...link, url_link }, request),
    ).toThrow();
});
