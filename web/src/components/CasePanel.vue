<script setup lang="ts">
import { ref, onMounted } from "vue";
import { ApiClient, displayValue } from "../api";
const props = defineProps<{
  client: ApiClient;
  resource: string;
  id: string;
  initialStatus: string;
}>();
const emit = defineEmits<{ updated: [] }>();
const comments = ref<Record<string, unknown>[]>([]),
  events = ref<Record<string, unknown>[]>([]),
  content = ref(""),
  reviewStatus = ref(""),
  reviewComment = ref(""),
  status = ref(props.initialStatus),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const base = `/resources/${props.resource}/${encodeURIComponent(props.id)}`;
async function load() {
  try {
    const data = await props.client.request<{
      items: Record<string, unknown>[];
      events: Record<string, unknown>[];
    }>(base + "/history");
    comments.value = data.items;
    events.value = data.events;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "处理记录读取失败";
  }
}
async function write(action: "comments" | "submit" | "review") {
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const body =
      action === "comments"
        ? { content: content.value }
        : action === "review"
          ? { status: reviewStatus.value, comment: reviewComment.value }
          : {};
    const result = await props.client.request<Record<string, unknown>>(
      base + "/" + action,
      { method: "POST", body },
    );
    if (action === "comments") content.value = "";
    if (result.status) status.value = String(result.status);
    notice.value =
      action === "comments"
        ? "本地处理备注已保存"
        : action === "submit"
          ? "已提交平台本地审核，尚未向上游渠道提交"
          : "平台本地审核结果已保存";
    await load();
    emit("updated");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "处理失败";
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <section class="case-panel">
    <h3>平台本地处理</h3>
    <p class="muted">
      本页面记录平台内部资料、审核和沟通，不代表已向上游渠道提交进件、回复投诉或执行退款。
    </p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <p>
      本地状态：<span class="badge">{{ status }}</span>
    </p>
    <button
      v-if="
        resource === 'onboarding-applications' &&
        ['draft', 'action_required', 'under_review'].includes(status)
      "
      :disabled="busy"
      @click="write('submit')"
    >
      提交平台本地审核
    </button>
    <form v-if="client.area === 'admin'" @submit.prevent="write('review')">
      <label
        >本地审核结果<select v-model="reviewStatus" required>
          <option value="">请选择</option>
          <option
            v-for="state in resource === 'complaints'
              ? ['processing', 'resolved', 'closed']
              : ['approved', 'rejected', 'action_required']"
            :key="state"
            :value="state"
          >
            {{ state }}
          </option>
        </select></label
      ><label
        >审核说明<textarea
          v-model="reviewComment"
          maxlength="10000"
          rows="3"
        /></label
      ><button :disabled="busy">保存本地审核结果</button>
    </form>
    <h3>处理备注</h3>
    <ol class="case-history">
      <li v-for="comment in comments" :key="String(comment.id)">
        <p>{{ displayValue(comment.content) }}</p>
        <small class="muted"
          >{{ comment.actor_name }} · {{ comment.created_at }}</small
        >
      </li>
      <li v-if="!comments.length" class="muted">尚无本地处理备注</li>
    </ol>
    <form @submit.prevent="write('comments')">
      <label
        >新增处理备注<textarea
          v-model="content"
          required
          maxlength="10000"
          rows="3"
        /></label
      ><button :disabled="busy || !content.trim()">保存处理备注</button>
    </form>
    <details class="audit-details">
      <summary>查看操作历史（{{ events.length }}）</summary>
      <ol class="case-history">
        <li v-for="event in events" :key="String(event.id)">
          <strong>{{ event.operation }}</strong>
          <p class="muted">{{ event.actor_name }} · {{ event.created_at }}</p>
        </li>
      </ol>
    </details>
  </section>
</template>
