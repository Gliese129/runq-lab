import { ref, watch } from 'vue'

const PREFIX = 'runq:'

function load<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(PREFIX + key)
    return raw ? JSON.parse(raw) : fallback
  } catch {
    return fallback
  }
}

function save(key: string, value: unknown) {
  localStorage.setItem(PREFIX + key, JSON.stringify(value))
}

// --- Preferred metric key per job ---

const preferredMetrics = ref<Record<string, string>>(load('preferred-metrics', {}))

watch(preferredMetrics, (v) => save('preferred-metrics', v), { deep: true })

function setPreferredMetric(jobId: string, key: string) {
  preferredMetrics.value[jobId] = key
  // Keep map small — only last 50 jobs
  const entries = Object.entries(preferredMetrics.value)
  if (entries.length > 50) {
    preferredMetrics.value = Object.fromEntries(entries.slice(-50))
  }
}

// --- Task status filter preference ---

const lastStatusFilter = ref(load('status-filter', ''))

watch(lastStatusFilter, (v) => save('status-filter', v))

// --- Compare sort preference ---

const compareSortDesc = ref(load('compare-desc', false))

watch(compareSortDesc, (v) => save('compare-desc', v))

// --- File browser: show hidden files (dotfiles) ---

const showHiddenFiles = ref(load('show-hidden-files', false))

watch(showHiddenFiles, (v) => save('show-hidden-files', v))

// --- Job task table: visible columns per job ---

const jobVisibleCols = ref<Record<string, string[]>>(load('job-cols', {}))

watch(jobVisibleCols, (v) => save('job-cols', v), { deep: true })

function setJobVisibleCols(jobId: string, cols: string[]) {
  jobVisibleCols.value[jobId] = cols
  const entries = Object.entries(jobVisibleCols.value)
  if (entries.length > 50) {
    jobVisibleCols.value = Object.fromEntries(entries.slice(-50))
  }
}

function getJobVisibleCols(jobId: string): string[] | null {
  return jobVisibleCols.value[jobId] || null
}

// --- Results table: row label template per PROJECT (the same sweep axes
// recur across a project's jobs, so the template follows the project) ---

const resultLabelTemplates = ref<Record<string, string>>(load('result-labels', {}))

watch(resultLabelTemplates, (v) => save('result-labels', v), { deep: true })

function setResultLabelTemplate(project: string, tpl: string) {
  resultLabelTemplates.value[project] = tpl
  const entries = Object.entries(resultLabelTemplates.value)
  if (entries.length > 50) {
    resultLabelTemplates.value = Object.fromEntries(entries.slice(-50))
  }
}

function getResultLabelTemplate(project: string): string {
  return resultLabelTemplates.value[project] || ''
}

export function usePreferences() {
  return {
    preferredMetrics,
    setPreferredMetric,
    lastStatusFilter,
    compareSortDesc,
    showHiddenFiles,
    setJobVisibleCols,
    getJobVisibleCols,
    setResultLabelTemplate,
    getResultLabelTemplate,
  }
}
