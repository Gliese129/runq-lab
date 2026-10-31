<template>
  <!-- What the view will look like once it has data: a faded table and a
       chart frame, with one line saying why it is empty (or nothing while
       it loads). -->
  <div class="view-skeleton" :class="{ loading: !message }" aria-hidden="false">
    <div class="sk-table" aria-hidden="true">
      <div class="sk-row head"><span v-for="c in 5" :key="c" :style="{ width: c === 1 ? '22%' : '12%' }" /></div>
      <div v-for="r in 4" :key="r" class="sk-row"><span v-for="c in 5" :key="c" :style="{ width: c === 1 ? `${14 + (r * 7) % 12}%` : `${7 + (r + c) % 4}%` }" /></div>
    </div>
    <div class="sk-chart" aria-hidden="true">
      <svg viewBox="0 0 400 120" preserveAspectRatio="none">
        <polyline points="0,100 60,82 120,88 180,60 240,52 300,40 360,30 400,26" />
        <polyline points="0,110 60,98 120,94 180,84 240,70 300,66 360,58 400,50" class="b" />
      </svg>
    </div>
    <div v-if="message" class="sk-message">
      <div>{{ message }}</div>
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ message?: string }>()
</script>

<style scoped>
.view-skeleton { position: relative; margin-top: 20px; }
.sk-table {
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant)); border-radius: var(--radius);
  background: rgb(var(--v-theme-surface)); padding: 4px 0;
}
.sk-row { display: flex; gap: 6%; align-items: center; height: var(--row-h); padding: 0 16px; }
.sk-row + .sk-row { border-top: var(--border-hairline) solid rgb(var(--v-theme-outline-variant), 0.6); }
.sk-row span { height: 8px; border-radius: 4px; background: rgb(var(--v-theme-on-surface), 0.07); }
.sk-row.head span { height: 7px; background: rgb(var(--v-theme-on-surface), 0.11); }
.sk-chart {
  margin-top: 14px; height: 140px; padding: 14px 16px; border-radius: var(--radius);
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant)); background: rgb(var(--v-theme-surface));
}
.sk-chart svg { width: 100%; height: 100%; }
.sk-chart polyline { fill: none; stroke: rgb(var(--v-theme-on-surface), 0.1); stroke-width: 2; vector-effect: non-scaling-stroke; }
.sk-chart polyline.b { stroke: rgb(var(--v-theme-on-surface), 0.06); }
.loading .sk-row span, .loading .sk-chart polyline { animation: sk-pulse 1.6s ease-in-out infinite; }
@keyframes sk-pulse { 50% { opacity: 0.45; } }
@media (prefers-reduced-motion: reduce) { .loading .sk-row span, .loading .sk-chart polyline { animation: none; } }
.sk-message {
  position: absolute; left: 50%; top: 42%; transform: translate(-50%, -50%); max-width: min(460px, 90%);
  text-align: center; font-size: var(--fs-base); line-height: 1.6; padding: 14px 20px; border-radius: var(--radius);
  background: rgb(var(--v-theme-surface)); border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
  color: rgb(var(--v-theme-on-surface-variant));
}
</style>
