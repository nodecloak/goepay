import { afterEach, describe, it, expect, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import BlacklistAddDialog from "../src/components/BlacklistAddDialog.vue";
import { ApiClient } from "../src/api";

const mounted: VueWrapper[] = [];
afterEach(() => mounted.splice(0).forEach((w) => w.unmount()));

function client() {
  return {
    area: "admin",
    request: vi.fn().mockResolvedValue({ created: ["1", "2"], skipped: 1 }),
  } as unknown as ApiClient;
}
function setup(c: ApiClient) {
  const w = mount(BlacklistAddDialog, {
    props: {
      client: c,
      kindField: {
        name: "kind",
        label: "拉黑类型",
        type: "select",
        options: [
          { value: "ip", label: "IP" },
          { value: "domain", label: "域名" },
          { value: "buyer", label: "买家" },
        ],
      },
    },
    global: {
      stubs: {
        // KitModal teleports to body; a passthrough stub keeps the content in
        // the wrapper and exposes submit/close so the serializer never recurses.
        KitModal: {
          props: ["loading", "submitDisabled"],
          emits: ["submit", "close"],
          template:
            '<form @submit.prevent="$emit(\'submit\')"><slot /><button type="submit" class="do-submit" :disabled="submitDisabled">添加</button></form>',
        },
      },
    },
  });
  mounted.push(w);
  return w;
}

describe("blacklist bulk-add dialog", () => {
  it("splits the textarea into lines, sends kind+days+remark, and reports the result", async () => {
    const c = client();
    const w = setup(c);
    await w.get("textarea").setValue("10.0.0.1\n10.0.0.2\n\n  10.0.0.3  \n");
    // 有效期 (the numeric input is the days field).
    await w.get('input[inputmode="numeric"]').setValue("7");
    // 备注 is the plain text input.
    await w.get("input:not([inputmode])").setValue("批量风控");
    // Live line count ignores blank/whitespace-only lines.
    expect(w.text()).toContain("当前有效行数：3");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(c.request).toHaveBeenCalledWith(
      "/resources/blacklist/batch-create",
      expect.objectContaining({
        method: "POST",
        body: {
          kind: "ip",
          lines: ["10.0.0.1", "10.0.0.2", "10.0.0.3"],
          days: 7,
          remark: "批量风控",
        },
      }),
    );
    expect(w.emitted("saved")?.[0][0]).toBe(
      "已添加 2 条黑名单，跳过 1 条已存在",
    );
  });

  it("refuses an empty content without calling the API", async () => {
    const c = client();
    const w = setup(c);
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(c.request).not.toHaveBeenCalled();
    expect(w.text()).toContain("请至少填写一行拉黑内容");
  });

  it("blocks submit on an out-of-range validity period", async () => {
    const c = client();
    const w = setup(c);
    await w.get("textarea").setValue("10.0.0.1");
    await w.get('input[inputmode="numeric"]').setValue("40000");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(c.request).not.toHaveBeenCalled();
    expect(w.text()).toContain("有效期需为 0-36500 的整数天（0 为永久）");
  });

  it("surfaces a server validation message naming the bad line", async () => {
    const c = client();
    vi.mocked(c.request).mockRejectedValueOnce(new Error("第 3 行内容无效"));
    const w = setup(c);
    await w.get("textarea").setValue("10.0.0.1\n10.0.0.2\nnot-an-ip");
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(w.text()).toContain("第 3 行内容无效");
    expect(w.emitted("saved")).toBeUndefined();
  });
});
