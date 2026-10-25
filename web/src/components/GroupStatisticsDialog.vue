<script setup lang="ts">
import type { ApiClient } from "../api";
import PaymentStatisticsDialog from "./PaymentStatisticsDialog.vue";
defineProps<{ client: ApiClient; groupID: string; ownSession: boolean }>();
const emit = defineEmits<{ close: [] }>();
</script>
<template>
  <PaymentStatisticsDialog
    :client="client"
    kind="group"
    :subject-id="groupID"
    :own-session="ownSession"
    @close="emit('close')"
  />
</template>
