import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import ConfigValueEditor from "../src/components/ConfigValueEditor.vue";

function editor(extra: Record<string, unknown> = {}) {
  const w = mount(ConfigValueEditor, {
    props: {
      label: "应用密钥",
      modelValue: "",
      operation: "keep",
      configured: true,
      secret: true,
      ...extra,
    },
  });
  const apply = async () => {
    const values = w.emitted("update:modelValue"),
      ops = w.emitted("update:operation");
    await w.setProps({
      ...(values?.length ? { modelValue: values.at(-1)![0] as string } : {}),
      ...(ops?.length ? { operation: ops.at(-1)![0] as string } : {}),
    });
  };
  return { w, apply };
}

describe("ordinary channel parameter controls", () => {
  it("shows public values directly without emitting a mutation or operation selector", async () => {
    const { w, apply } = editor({
      label: "应用ID",
      secret: false,
      currentValue: "original-app",
    });
    expect(w.get("input").element.value).toBe("original-app");
    expect(w.find("select").exists()).toBe(false);
    expect(w.emitted()).toEqual({});
    await w.get("input").setValue("new-app");
    await apply();
    expect(w.props("operation")).toBe("replace");
    await w.get("input").setValue("original-app");
    await apply();
    expect(w.props("operation")).toBe("keep");
    await w.get("input").setValue("");
    await apply();
    expect(w.props("operation")).toBe("clear");
    expect(w.get('[role="status"]').text()).toBe("保存后将清除");
    await w.get('button[aria-label="撤销清除应用ID"]').trigger("click");
    await apply();
    expect(w.get("input").element.value).toBe("original-app");
    expect(w.props("operation")).toBe("keep");
    w.unmount();
  });

  it("uses a blank secret input; draft deletion preserves the saved value and clear has undo", async () => {
    const { w, apply } = editor();
    expect(w.get("input").attributes("type")).toBe("password");
    expect(w.get("input").element.value).toBe("");
    expect(w.get("input").attributes("placeholder")).toBe(
      "已配置，输入新值可修改",
    );
    await w.get("input").setValue("new-private-value");
    await apply();
    expect(w.props("operation")).toBe("replace");
    await w.get("input").setValue("");
    await apply();
    expect(w.props("operation")).toBe("keep");
    await w.get('button[aria-label="清除应用密钥"]').trigger("click");
    await apply();
    expect(w.props("operation")).toBe("clear");
    expect(w.props("modelValue")).toBe("");
    await w.get('button[aria-label="撤销清除应用密钥"]').trigger("click");
    await apply();
    expect(w.props("operation")).toBe("keep");
    expect(w.props("modelValue")).toBe("");
    expect(w.html()).not.toContain("new-private-value");
    w.unmount();
  });

  it("shows current option values, inferring replacement only after an actual edit", async () => {
    const { w, apply } = editor({
      label: "签名方式",
      secret: false,
      currentValue: "MD5",
      options: [
        { value: "MD5", label: "MD5" },
        { value: "RSA", label: "RSA" },
      ],
    });
    expect(w.get("select").element.value).toBe("MD5");
    await w.get("select").setValue("RSA");
    await apply();
    expect(w.props("operation")).toBe("replace");
    expect(w.props("modelValue")).toBe("RSA");
    w.unmount();
  });

  it("cannot preserve or clear a missing required field or one belonging to another parent", async () => {
    const { w, apply } = editor({
      operation: "replace",
      configured: false,
      keepAllowed: false,
      clearAllowed: false,
      multiline: true,
    });
    expect(w.findAll("button")).toHaveLength(0);
    await w.get("textarea").setValue("new-parent-secret");
    await apply();
    await w.get("textarea").setValue("");
    await apply();
    expect(w.props("operation")).toBe("replace");
    expect(w.get("textarea").element.value).toBe("");
    w.unmount();
  });

  it("keeps PEM drafts editable while preserving disabled and teardown behavior", async () => {
    const { w } = editor({ multiline: true, disabled: true });
    expect(w.get("textarea").element.value).toBe("");
    expect(w.get("textarea").attributes("disabled")).toBeDefined();
    expect(w.get("button").attributes("disabled")).toBeDefined();
    w.unmount();
  });
});
