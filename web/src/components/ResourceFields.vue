<script setup lang="ts">
import type { ResourceField } from "../resources";
import { readonly } from "../resources";
import AdminPermissionField from "./AdminPermissionField.vue";
import { adminPermissionCatalog } from "../admin-permission-context";
defineProps<{
  fields: ResourceField[];
  values: Record<string, unknown>;
  errors: Record<string, string>;
  editing: boolean;
}>();
defineEmits<{ change: [name: string, value: unknown] }>();
</script>
<template>
  <div class="resource-fields">
    <template v-for="field in fields" :key="field.name"
      ><label
        v-if="!readonly(field, editing)"
        :class="{ 'checkbox-field': field.type === 'boolean' }"
        :for="'field-' + field.name"
        >{{ field.label
        }}<span
          v-if="
            field.required &&
            !(editing && (field.type === 'password' || field.write_only))
          "
          class="required-mark"
        >
          *</span
        ><input
          v-if="field.type === 'boolean'"
          :id="'field-' + field.name"
          type="checkbox"
          :checked="!!values[field.name]"
          @change="
            $emit(
              'change',
              field.name,
              ($event.target as HTMLInputElement).checked,
            )
          "
        /><AdminPermissionField
          v-else-if="field.type === 'permission_tree'"
          :catalog="adminPermissionCatalog"
          :value="(values[field.name] as string[]) || []"
          :disabled="values['admin_class'] === 'super'"
          @change="$emit('change', field.name, $event)"
        /><select
          v-else-if="field.options?.length || field.type === 'select'"
          :id="'field-' + field.name"
          :value="values[field.name]"
          @change="
            $emit(
              'change',
              field.name,
              ($event.target as HTMLSelectElement).value,
            )
          "
        >
          <option value="">请选择</option>
          <option
            v-for="option in field.options"
            :key="String(typeof option === 'string' ? option : option.value)"
            :value="typeof option === 'string' ? option : option.value"
          >
            {{ typeof option === "string" ? option : option.label }}
          </option></select
        ><textarea
          v-else-if="['text', 'json'].includes(field.type)"
          :id="'field-' + field.name"
          :value="String(values[field.name] ?? '')"
          :maxlength="field.max_length"
          rows="5"
          @input="
            $emit(
              'change',
              field.name,
              ($event.target as HTMLTextAreaElement).value,
            )
          "
        /><input
          v-else
          :id="'field-' + field.name"
          :value="values[field.name]"
          :type="
            ['password', 'email', 'url', 'date'].includes(field.type)
              ? field.type
              : 'text'
          "
          :autocomplete="field.type === 'password' ? 'new-password' : 'off'"
          :inputmode="
            ['money', 'number', 'integer'].includes(field.type)
              ? 'decimal'
              : undefined
          "
          :maxlength="field.max_length"
          :aria-invalid="!!errors[field.name]"
          :aria-describedby="
            errors[field.name] ? 'error-' + field.name : undefined
          "
          @input="
            $emit(
              'change',
              field.name,
              ($event.target as HTMLInputElement).value,
            )
          "
        /><small v-if="field.description" class="field-help">{{
          field.description
        }}</small
        ><small
          v-if="errors[field.name]"
          :id="'error-' + field.name"
          class="field-error"
          role="alert"
          >{{ errors[field.name] }}</small
        ></label
      ></template
    >
  </div>
</template>
