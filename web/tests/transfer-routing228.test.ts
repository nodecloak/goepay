// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Panel from "../src/components/TransferRoutingSettings228.vue";
import KitSelect from "../src/components/KitSelect.vue";

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
});
async function fixture() {
  let state = {
    fingerprint: "a".repeat(64),
    revision: "0",
    routes: {
      alipay: "inherit",
      wxpay: "inherit",
      qqpay: "inherit",
      bank: "inherit",
    },
  };
  const requests: { method: string; body: any }[] = [];
  const control = { unknown: false, malformed: false };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const method = init?.method || "GET",
        body = init?.body ? JSON.parse(String(init.body)) : undefined;
      requests.push({ method, body });
      if (method === "PATCH") {
        state = {
          fingerprint: "b".repeat(64),
          revision: "1",
          routes: body.routes,
        };
        if (control.unknown) throw Error("network result unknown");
        if (control.malformed)
          return new Response("<html>truncated response</html>", {
            status: 200,
          });
      }
      return new Response(
        JSON.stringify({
          code: 0,
          data: {
            state,
            channels: [
              { id: "9007199254740993", name: "本地通道", types: ["wxpay"] },
            ],
          },
        }),
        { status: 200 },
      );
    }),
  );
  const client = new ApiClient("admin");
  client.csrfToken = "local-csrf";
  const w = mount(Panel, {
    attachTo: document.body,
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  return { w, requests, control };
}
it("keeps full channel IDs and requires explicit confirmation, then reads back saved configuration", async () => {
  const { w, requests } = await fixture();
  try {
    expect(requests.map((r) => r.method)).toEqual(["GET"]);
    w.findAllComponents(KitSelect)[1]!.vm.$emit(
      "update:modelValue",
      "channel:9007199254740993",
    );
    await flushPromises();
    await w
      .findAll("button")
      .find((b) => b.text() === "保存转账通道")!
      .trigger("click");
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();
    (document.querySelector(".pa-modal-x") as HTMLButtonElement).click();
    await flushPromises();
    expect(requests.filter((r) => r.method === "PATCH")).toHaveLength(0);
    await w
      .findAll("button")
      .find((b) => b.text() === "保存转账通道")!
      .trigger("click");
    await flushPromises();
    (
      document.querySelector(
        '.pa-modal-foot button[type="submit"]',
      ) as HTMLButtonElement
    ).click();
    await flushPromises();
    expect(requests.at(-1)).toEqual({
      method: "PATCH",
      body: {
        expected_fingerprint: "a".repeat(64),
        routes: {
          alipay: "inherit",
          wxpay: "channel:9007199254740993",
          qqpay: "inherit",
          bank: "inherit",
        },
      },
    });
    expect(w.text()).toContain("未发起任何转账");
  } finally {
    w.unmount();
  }
});
for (const failure of ["network", "malformed"])
  it(`never repeats ${failure} unknown save and reconciles through an explicit GET`, async () => {
    const { w, requests, control } = await fixture();
    try {
      control.unknown = failure === "network";
      control.malformed = failure === "malformed";
      w.findAllComponents(KitSelect)[1]!.vm.$emit(
        "update:modelValue",
        "manual",
      );
      await flushPromises();
      await w
        .findAll("button")
        .find((b) => b.text() === "保存转账通道")!
        .trigger("click");
      await flushPromises();
      (
        document.querySelector(
          '.pa-modal-foot button[type="submit"]',
        ) as HTMLButtonElement
      ).click();
      await flushPromises();
      expect(w.text()).toContain("保存结果尚未核实");
      expect(
        w
          .findAll("button")
          .find((b) => b.text() === "保存转账通道")!
          .attributes("disabled"),
      ).toBeDefined();
      await w
        .findAll("button")
        .find((b) => b.text() === "重新读取转账通道")!
        .trigger("click");
      await flushPromises();
      expect(requests.map((r) => r.method)).toEqual(["GET", "PATCH", "GET"]);
      expect(w.text()).not.toContain("保存结果尚未核实");
      expect(w.text()).toContain("版本 1");
      expect(w.find('[role="alert"]').exists()).toBe(false);
      expect(w.findAllComponents(KitSelect)[1]!.props("modelValue")).toBe(
        "manual",
      );
    } finally {
      w.unmount();
    }
  });
it("clears state when own-session authority is lost and performs no delegated read", async () => {
  const { w, requests } = await fixture();
  try {
    await w.setProps({ ownSession: false });
    await flushPromises();
    expect(w.findAllComponents(KitSelect)).toHaveLength(0);
    expect(w.text()).toContain("仅超级管理员本人会话");
    expect(requests).toHaveLength(1);
  } finally {
    w.unmount();
  }
});
