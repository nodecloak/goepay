import { describe, it, expect } from "vitest";
import {
  validateFields,
  buildPayload,
  type ResourceSchema,
} from "../src/resources";
const schema: ResourceSchema = {
  name: "domains",
  title: "授权域名",
  permissions: { create: true, update: true, delete: true },
  fields: [
    {
      name: "domain",
      label: "域名",
      type: "string",
      required: true,
      max_length: 253,
    },
    { name: "enabled", label: "启用", type: "boolean" },
    { name: "weight", label: "权重", type: "integer", min: 1, max: 100 },
    { name: "config", label: "配置", type: "json" },
  ],
};
describe("typed resource forms", () => {
  it("allows merchant IDs on creation and excludes immutable IDs from edits", () => {
    const s: ResourceSchema = {
      title: "商户",
      fields: [
        {
          name: "id",
          label: "商户 ID",
          type: "string",
          required: true,
          create_only: true,
        },
      ],
      permissions: { create: true, update: true, delete: false },
    };
    expect(validateFields(s, {})).toHaveProperty("id");
    expect(buildPayload(s, { id: "1000" })).toEqual({ id: "1000" });
    expect(validateFields(s, {}, true)).toEqual({});
    expect(buildPayload(s, { id: "1000" }, true)).toEqual({});
  });
  it("allows clearing optional text while retaining hidden passwords on edit", () => {
    const s: ResourceSchema = {
      title: "账户",
      permissions: { create: true, update: true, delete: false },
      fields: [
        { name: "note", label: "备注", type: "string" },
        { name: "password", label: "密码", type: "password", write_only: true },
      ],
    };
    expect(buildPayload(s, { note: "", password: "" }, true)).toEqual({
      note: "",
    });
  });
  it("rejects missing fields, fractional integers, and malformed JSON", () => {
    expect(
      validateFields(schema, { domain: "", weight: "1.2", config: "{bad" }),
    ).toMatchObject({
      domain: expect.any(String),
      weight: expect.any(String),
      config: expect.any(String),
    });
  });
  it("preserves booleans and validates integer bounds without dropping false", () => {
    expect(
      validateFields(schema, {
        domain: "pay.example.com",
        enabled: false,
        weight: "101",
      }),
    ).toHaveProperty("weight");
    expect(
      buildPayload(schema, {
        domain: "pay.example.com",
        enabled: false,
        weight: "2",
        config: '{"x":1}',
      }),
    ).toEqual({
      domain: "pay.example.com",
      enabled: false,
      weight: 2,
      config: { x: 1 },
    });
  });
  it("does not include read-only or unknown fields in writes", () => {
    const s = {
      ...schema,
      fields: [
        ...schema.fields,
        {
          name: "balance",
          label: "余额",
          type: "string" as const,
          read_only: true,
        },
      ],
    };
    expect(
      buildPayload(s, {
        domain: "example.com",
        balance: "100",
        injected: "value",
      }),
    ).not.toHaveProperty("balance");
    expect(
      buildPayload(s, { domain: "example.com", injected: "value" }),
    ).not.toHaveProperty("injected");
  });
});
