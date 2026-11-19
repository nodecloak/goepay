// @vitest-environment jsdom
import { it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import CollectionGeneratorPage from "../src/components/CollectionGeneratorPage.vue";
import type { ApiClient } from "../src/api";

vi.mock("qrcode", () => ({
  default: { toDataURL: vi.fn(async () => "data:image/png;base64,local") },
}));

beforeEach(() => {
  // jsdom lacks randomUUID in some runners; the page uses it on generate.
  if (!("randomUUID" in crypto)) {
    (crypto as unknown as { randomUUID: () => string }).randomUUID = () =>
      "00000000-0000-4000-8000-000000000000";
  }
});

function client(request: ReturnType<typeof vi.fn>): ApiClient {
  return {
    area: "merchant",
    csrfToken: "csrf",
    request,
  } as unknown as ApiClient;
}

// F10: the generator prefills the name with the stored default codename and
// exposes a 保存为默认 action that persists it via PATCH (account/editCodename).
it("prefills and saves the default aggregate-code name", async () => {
  const request = vi.fn(
    async (path: string, opts?: { method?: string; body?: unknown }) => {
      if (
        path === "/collection-generator" &&
        (!opts || opts.method === undefined)
      )
        return {
          recipient_name: "我的收款",
          default_codename: "我的收款",
          codename_customized: true,
          styles: [],
          domains: [
            { origin: "https://pay.example", label: "https://pay.example" },
          ],
          latest: null,
        };
      if (opts?.method === "PATCH")
        return { default_codename: "新的默认名", recipient_name: "新的默认名" };
      throw new Error(
        "unexpected call " + path + " " + (opts?.method ?? "GET"),
      );
    },
  );
  const w = mount(CollectionGeneratorPage, {
    props: { client: client(request), ownSession: true },
    global: { stubs: { MerchantScanShortcut192: true, ResourcePage: true } },
  });
  await flushPromises();

  // The name field is prefilled with the stored default.
  const nameInput = w.get('input[maxlength="100"]');
  expect((nameInput.element as HTMLInputElement).value).toBe("我的收款");
  expect(w.text()).toContain("默认收款名称：我的收款");
  expect(w.text()).toContain("已自定义");

  // Editing + 保存为默认 issues the editCodename PATCH with the typed name.
  await nameInput.setValue("新的默认名");
  const saveBtn = w.findAll("button").find((b) => b.text() === "保存为默认")!;
  expect(saveBtn).toBeTruthy();
  await saveBtn.trigger("click");
  await flushPromises();

  const patch = request.mock.calls.find((c) => c[1]?.method === "PATCH");
  expect(patch).toBeTruthy();
  expect(patch?.[1]?.body).toEqual({ recipient_name: "新的默认名" });
  expect(w.text()).toContain("默认收款名称已保存");
  w.unmount();
});

it("withholds result actions when the returned URL belongs to a different origin", async () => {
  const request = vi.fn(async () => ({
    recipient_name: "本地商户",
    styles: [],
    domains: [{ origin: "https://pay.example", label: "收款域名" }],
    latest: {
      code_id: "42",
      recipient_name: "本地商户",
      origin: "https://pay.example",
      style_id: "0",
      fixed_amount_cents: "0",
      url: "https://unlinked.example.invalid/collect/42",
      enabled: true,
    },
  }));
  const w = mount(CollectionGeneratorPage, {
    props: { client: client(request), ownSession: true },
    global: { stubs: { MerchantScanShortcut192: true, ResourcePage: true } },
  });
  await flushPromises();

  expect(w.get('[role="alert"]').text()).toContain("收款链接格式无效");
  expect(w.find('a[href*="unlinked.example.invalid"]').exists()).toBe(false);
  expect(w.text()).not.toContain("复制收款链接");
  expect(w.text()).not.toContain("下载收款码");
  w.unmount();
});

it("keeps the generator disabled when the setup response is incomplete", async () => {
  const request = vi.fn(async () => ({
    recipient_name: "未核对的默认名称",
    domains: null,
    latest: null,
  }));
  const renderError = vi.fn();
  const w = mount(CollectionGeneratorPage, {
    props: { client: client(request), ownSession: true },
    global: {
      config: { errorHandler: renderError },
      stubs: { MerchantScanShortcut192: true, ResourcePage: true },
    },
  });
  await flushPromises();

  expect(renderError).not.toHaveBeenCalled();
  expect(w.get('[role="alert"]').text()).toBe(
    "收款码配置响应无效，请重新进入页面。",
  );
  expect(w.text()).not.toContain("未核对的默认名称");
  expect(w.get('button[type="submit"]').attributes()).toHaveProperty(
    "disabled",
  );
  w.unmount();
});
