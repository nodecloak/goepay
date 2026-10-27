import { it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import ResourceFields from "../src/components/ResourceFields.vue";
import {
  buildPayload,
  formValues,
  type ResourceSchema,
} from "../src/resources";
import schemas from "../e2e/domain166-schemas.json";
for (const area of ["admin", "merchant"] as const)
  it(`domain166 ${area} consumes the actual server field help and preserves the wildcard input`, () => {
    const schema = schemas[area] as ResourceSchema;
    const values = {
      ...formValues(schema),
      merchant_id: "1",
      domain: "*.Example.COM.",
    };
    const view = mount(ResourceFields, {
      props: { fields: schema.fields, values, errors: {}, editing: false },
    });
    expect(view.text()).toContain("*.example.com");
    expect(view.text()).toContain("不含根域名");
    expect((view.get("#field-domain").element as HTMLInputElement).value).toBe(
      "*.Example.COM.",
    );
    expect(buildPayload(schema, values, false).domain).toBe("*.Example.COM.");
    expect(view.find("#field-approved").exists()).toBe(area === "admin");
    view.unmount();
  });
