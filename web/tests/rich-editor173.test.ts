import { it, expect } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import RichTextEditor from "../src/components/RichTextEditor.vue";
it("edits HTML only after apply, sanitizes it and cancel does not emit", async () => {
  const w = mount(RichTextEditor, {
    props: { modelValue: "<p>原文</p>", label: "公告内容" },
    attachTo: document.body,
  });
  await w.get('[data-rich-command="html"]').trigger("click");
  await w
    .get("textarea")
    .setValue(
      '<p style="color:#123456" onclick="bad()">新内容</p><script>bad()</script>',
    );
  expect(w.emitted("update:modelValue")).toBeUndefined();
  await w.get('[data-rich-source="cancel"]').trigger("click");
  expect(w.emitted("update:modelValue")).toBeUndefined();
  await w.get('[data-rich-command="html"]').trigger("click");
  await w
    .get("textarea")
    .setValue('<p onclick="bad()">新内容</p><script>bad()</script>');
  await w.get('[data-rich-source="apply"]').trigger("click");
  expect(w.emitted("update:modelValue")?.at(-1)).toEqual(["<p>新内容</p>"]);
  expect(w.find("script").exists()).toBe(false);
  w.unmount();
});
it("filters clipboard content before insertion and honors disabled editing", async () => {
  const w = mount(RichTextEditor, {
    props: { modelValue: "", label: "公告内容" },
    attachTo: document.body,
  });
  const el = w.get("[contenteditable]");
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      getData: (t: string) =>
        t === "text/html"
          ? '<b>欢迎</b><img src="https://tracker.invalid/a.png" onerror="bad()">'
          : "",
    },
  });
  el.element.dispatchEvent(event);
  await flushPromises();
  expect(event.defaultPrevented).toBe(true);
  expect(el.html()).toContain("<b>欢迎</b>");
  expect(el.html()).not.toContain("tracker");
  expect(w.emitted("update:modelValue")?.at(-1)).toEqual(["<b>欢迎</b>"]);
  await w.setProps({ disabled: true });
  expect(el.attributes("contenteditable")).toBe("false");
  expect(
    w.findAll("button").every((b) => b.attributes("disabled") !== undefined),
  ).toBe(true);
  w.unmount();
});
