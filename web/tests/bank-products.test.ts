import { describe, it, expect } from "vitest";
import { bankChannelProduct as product } from "../src/transfers";
describe("explicit product-specific bank capability", () => {
  it("accepts only consistent explicit product and code requirements", () => {
    expect(
      product({
        types: ["bank"],
        bank_product: "alipay_bankcard",
        bank_code_required: false,
      }),
    ).toBe("alipay_bankcard");
    expect(
      product({
        types: ["bank"],
        bank_product: "wxpay_v2",
        bank_code_required: true,
      }),
    ).toBe("wxpay_v2");
    for (const channel of [
      null,
      {},
      { types: ["bank"] },
      { types: ["bank"], bank_product: "other", bank_code_required: false },
      {
        types: ["bank"],
        bank_product: "alipay_bankcard",
        bank_code_required: true,
      },
      { types: ["bank"], bank_product: "wxpay_v2", bank_code_required: false },
      {
        types: ["alipay"],
        bank_product: "alipay_bankcard",
        bank_code_required: false,
      },
    ])
      expect(product(channel)).toBeUndefined();
  });
});
