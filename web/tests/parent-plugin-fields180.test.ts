import { expect, it } from "vitest";
import {
  readParentChannel,
  readParentChannelList,
  readParentTemplates,
} from "../src/parent-channels";

// A ported plugin serves a catalog pay type under its OWN plugin name, so
// pay_type and plugin differ. The parser must accept that and the plugin's
// field label/note/options.
const configured = {
  id: "12",
  name: "易支付通道",
  pay_type: "qqpay",
  plugin: "easypay",
  configured: true,
  source_channel_id: "12",
  revision: "1",
  account_version: "1",
  enabled: false,
  archived: false,
  mode: 0,
  rate: "0.60",
  fee_rate_bps: 9940,
  fingerprint: "a".repeat(64),
  account: {
    plugin: "easypay",
    app_types: [],
    mode: 0,
    fields: [
      {
        namespace: "config",
        name: "merchant_id",
        secret: false,
        pem: false,
        present: true,
        value: "888",
        label: "商户号",
        note: "易支付后台的商户 ID",
      },
      {
        namespace: "config",
        name: "sign_type",
        secret: false,
        pem: false,
        present: true,
        value: "MD5",
        label: "签名方式",
        options: [
          { value: "MD5", label: "MD5" },
          { value: "RSA", label: "RSA" },
        ],
      },
      {
        namespace: "config",
        name: "api_key",
        secret: true,
        pem: false,
        present: true,
      },
    ],
  },
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
};

it("accepts plugin distinct from pay_type with a catalog pay type", () => {
  const r = readParentChannel(configured);
  expect(r.pay_type).toBe("qqpay");
  expect(r.plugin).toBe("easypay");
  expect(r.account?.fields[0].label).toBe("商户号");
  expect(r.account?.fields[0].note).toBe("易支付后台的商户 ID");
  expect(r.account?.fields[1].options?.map((o) => o.value)).toEqual([
    "MD5",
    "RSA",
  ]);
});

it("rejects a non-catalog pay type, an invalid plugin name and a mismatched account plugin", () => {
  for (const raw of [
    { ...configured, pay_type: "notatype" },
    { ...configured, plugin: "Bad-Name" },
    { ...configured, account: { ...configured.account, plugin: "other" } },
    {
      ...configured,
      account: {
        ...configured.account,
        fields: [
          {
            namespace: "config",
            name: "x",
            secret: false,
            pem: false,
            present: true,
            options: [
              { value: "a", label: "A" },
              { value: "a", label: "B" },
            ],
          },
        ],
      },
    },
  ])
    expect(() => readParentChannel(raw)).toThrow();
});

it("parses an unconfigured native channel and a ported template", () => {
  expect(
    readParentChannelList({
      items: [
        {
          id: "3",
          name: "原通道",
          pay_type: "alipay",
          plugin: "alipay",
          configured: false,
        },
      ],
      total: 1,
      page: 1,
      size: 20,
    }).items,
  ).toHaveLength(1);
  const tmpl = readParentTemplates({
    items: [
      {
        source_channel_id: "7",
        name: "原授权通道 #7",
        pay_type: "bank",
        fields: [
          {
            namespace: "config",
            name: "mch",
            secret: false,
            pem: false,
            label: "商户",
          },
        ],
        fingerprint: "b".repeat(64),
        merchant_count: 2,
      },
    ],
    total: 1,
  });
  expect(tmpl[0].pay_type).toBe("bank");
  expect(tmpl[0].fields[0].label).toBe("商户");
});
