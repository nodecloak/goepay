<script setup lang="ts">
defineProps<{ kind: "iptype" | "advanced" }>();
</script>

<template>
  <section class="settings-fields" aria-label="部署配置说明">
    <div v-if="kind === 'iptype'" class="setting-field">
      <label for="settings-runtime-client-ip">用户IP地址获取方式</label>
      <input
        id="settings-runtime-client-ip"
        aria-label="用户IP地址获取方式"
        value="直连来源与受信任代理规则"
        readonly
      />
      <small class="muted"
        >仅对明确授权的直连代理接收单个
        X-Real-IP；其他来源使用连接地址。代理信任范围随部署配置生效。</small
      >
    </div>
    <template v-else>
      <div class="setting-field">
        <label for="settings-runtime-admin-path">后台路径</label>
        <input
          id="settings-runtime-admin-path"
          aria-label="后台路径"
          value="/admin"
          readonly
        />
        <small class="muted"
          >后台路径由应用路由与反向代理共同管理，调整需随服务部署同步完成。</small
        >
      </div>
      <div class="setting-field">
        <label for="settings-runtime-cache">缓存驱动</label>
        <input
          id="settings-runtime-cache"
          aria-label="缓存驱动"
          value="由服务部署配置管理"
          readonly
        />
        <small class="muted"
          >缓存策略随服务运行配置生效，此处显示配置方式。</small
        >
      </div>
    </template>
  </section>
</template>
