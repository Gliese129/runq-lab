<template>
  <div class="results-empty">
    <div class="text-subtitle-1 mb-1">{{ t('results.none_title') }}</div>
    <div class="text-body-2 text-on-surface-variant mb-4">{{ t('results.none_body') }}</div>

    <template v-if="views.length">
      <div class="text-caption text-on-surface-variant mb-1">{{ t('results.views_waiting') }}</div>
      <ul class="waiting mb-4">
        <li v-for="v in views" :key="v.name">
          <router-link :to="{ name: 'view', params: { project, name: v.name } }">{{ v.title || v.name }}</router-link>
          <span v-if="v.spec?.section" class="text-on-surface-variant">
            · {{ t('results.reads_section') }} <code>{{ v.spec.section }}</code>
          </span>
        </li>
      </ul>
    </template>

    <div class="text-caption text-on-surface-variant mb-1">{{ example.forView ? t('results.example_for', { name: example.forView }) : t('results.example') }}</div>
    <div class="example">
      <code>{{ example.code }}</code>
      <v-btn size="x-small" variant="text" icon :aria-label="t('task.copy_detail')" @click="copy"><v-icon size="14">mdi-content-copy</v-icon></v-btn>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useProjectViewSpecsQuery } from '@/queries/useViewQueries'
import { useSnackbar } from '@/composables/useSnackbar'

const props = defineProps<{ project: string }>()
const { t } = useI18n()
const snack = useSnackbar()
const specsQuery = useProjectViewSpecsQuery(() => props.project)
const views = computed(() => specsQuery.data.value ?? [])

/** A record call shaped like the first view's section and row fields, so
 *  copying it is exactly what makes that view appear for this job. */
const example = computed(() => {
  const v = views.value.find((x) => x.spec?.section)
  if (!v?.spec) return { forView: '', code: 'runq.record({"acc": acc, "scored": n}, section="eval", split=split)' }
  const s = v.spec as { section?: string; rows?: string[]; columns?: { value?: string }[] }
  const axes = (s.rows ?? []).filter((k) => !k.startsWith('param.')).map((k) => `${k}=${k}`)
  const fields = [...new Set((s.columns ?? []).flatMap((c) => (c.value ?? '').match(/[A-Za-z_][A-Za-z0-9_]*/g) ?? []))]
    .filter((f) => !['and', 'or', 'not'].includes(f)).slice(0, 4)
  const metrics = (fields.length ? fields : ['value']).map((f) => `"${f}": ${f}`).join(', ')
  return { forView: v.title || v.name, code: `runq.record({${metrics}}, section="${s.section}"${axes.length ? ', ' + axes.join(', ') : ''})` }
})

async function copy() {
  try {
    await navigator.clipboard.writeText(example.value.code)
    snack.success(t('common.copied'))
  } catch { /* clipboard blocked */ }
}
</script>

<style scoped>
.results-empty { padding: 20px 4px; max-width: 760px; }
.waiting { list-style: none; padding: 0; margin: 0; font-size: var(--fs-base); }
.waiting li { padding: 2px 0; }
.waiting code, .example code { font-family: var(--font-mono); font-size: var(--fs-meta); background: none; }
.example {
  display: flex; align-items: center; gap: 6px; padding: 8px 10px; border-radius: 8px;
  background: rgb(var(--v-theme-on-surface), 0.04); overflow-x: auto;
}
.example { align-items: flex-start; }
.example code { white-space: pre-wrap; overflow-wrap: anywhere; flex: 1; }
</style>
