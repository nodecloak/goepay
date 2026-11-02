<script setup lang="ts">
import { ApiClient } from "../api";
import KYCSettingsPanel233 from "./KYCSettingsPanel233.vue";
defineProps<{
  client: ApiClient;
  ownSession: boolean;
  adminClass?: string;
  disabled?: boolean;
  active?: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
</script>
<template>
  <KYCSettingsPanel233
    v-bind="$props"
    kind="ocr"
    @busy="emit('busy', $event)"
  />
</template>
