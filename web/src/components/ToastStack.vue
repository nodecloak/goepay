<script setup lang="ts">
import ShellIcon from "./ShellIcon.vue";
import { useToasts } from "../toast";
const { toasts, dismissToast } = useToasts();
const icon: Record<string, string> = {
  success: "st-badge",
  warning: "st-alert",
  danger: "st-alert",
  info: "info",
};
</script>
<template>
  <div class="toast-stack" aria-live="polite" aria-atomic="false">
    <div
      v-for="toast in toasts"
      :key="toast.id"
      class="toast"
      :class="'toast--' + toast.tone"
      role="status"
      @click="dismissToast(toast.id)"
    >
      <ShellIcon :name="icon[toast.tone] || 'info'" />
      <span>{{ toast.text }}</span>
    </div>
  </div>
</template>
