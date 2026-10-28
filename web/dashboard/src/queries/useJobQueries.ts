import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { jobsApi } from '@/apis/jobs'
import { useConfigStore } from '@/stores/config'
import { useCancelling } from '@/composables/useCancelling'
import { qk } from './keys'
import type { JobDetail } from '@/types/api'

// List pages tolerate 5s freshness (the old Overview poll); detail pages
// track live jobs at 3s. Poll-model targets reconcile server-side every
// ~30s — polling faster than the data source is just login-node noise.
const LIST_POLL_MS = 5_000

function detailPollMs(isPoll: boolean): number {
  return isPoll ? 30_000 : 3_000
}

const ACTIVE_JOB = new Set(['running', 'pending', 'paused'])

export function useJobsListQuery() {
  return useQuery({
    queryKey: qk.jobs,
    queryFn: ({ signal }) => jobsApi.list({ silent: true, signal }),
    refetchInterval: LIST_POLL_MS,
  })
}

export function useArchivedJobsQuery(project: MaybeRefOrGetter<string>, open: MaybeRefOrGetter<boolean>, page: MaybeRefOrGetter<number>) {
  return useQuery({
    queryKey: computed(() => [...qk.jobsArchived, toValue(project), toValue(page)]),
    queryFn: () => jobsApi.listArchived(toValue(project), toValue(page)),
    enabled: computed(() => toValue(open)),
    staleTime: 60_000,
    refetchInterval: false,
  })
}

/** Project-scoped list — skips the archived-project cascade (see jobsApi). */
export function useProjectJobsQuery(project: MaybeRefOrGetter<string>, live: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: computed(() => qk.projectJobs(toValue(project))),
    queryFn: ({ signal }) => jobsApi.listByProject(toValue(project), { silent: true, signal }),
    refetchInterval: computed(() => toValue(live) ? LIST_POLL_MS : false),
  })
}

export function useJobDetailQuery(jobId: MaybeRefOrGetter<string>) {
  const config = useConfigStore()
  return useQuery({
    queryKey: computed(() => qk.job(toValue(jobId))),
    queryFn: ({ signal }) => jobsApi.get(toValue(jobId), { silent: true, signal }),
    // Live only while the job is: terminal jobs stop polling entirely.
    refetchInterval: (query) => {
      const s = query.state.data?.job.status
      return s && ACTIVE_JOB.has(s) ? detailPollMs((query.state.data?.capabilities ?? config.capabilitiesFor(query.state.data?.job.target)).state_model === 'poll') : false
    },
  })
}

/** Per-task log activity (cumulative bytes per minute) — the source of
 *  "last output N ago". Polled at the sampling interval while the job is
 *  live; fetched once for settled jobs. */
const ACTIVITY_POLL_MS = 60_000

export function useJobActivityQuery(
  jobId: MaybeRefOrGetter<string>,
  live: MaybeRefOrGetter<boolean>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  return useQuery({
    enabled: computed(() => toValue(enabled)),
    queryKey: computed(() => qk.activity(toValue(jobId))),
    queryFn: () => jobsApi.activity(toValue(jobId)),
    refetchInterval: computed(() => (toValue(live) ? ACTIVITY_POLL_MS : false)),
    staleTime: ACTIVITY_POLL_MS,
  })
}

/** Read-only view of the SAME cache entry as useJobDetailQuery, for pages
 *  that need job CONTEXT rather than live job state — the task page's W&B
 *  base_url, target name, and sibling params (swept-vs-fixed). Those are
 *  static for the life of a job, so this observer never polls and treats
 *  cached data as fresh: arriving from the job page costs zero requests,
 *  and a deep link pays exactly one. Sharing the key means the job page's
 *  own polling observer still refreshes the entry for both. */
const JOB_CONTEXT_STALE_MS = 5 * 60_000

export function useJobContextQuery(jobId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => qk.job(toValue(jobId))),
    queryFn: ({ signal }) => jobsApi.get(toValue(jobId), { silent: true, signal }),
    staleTime: JOB_CONTEXT_STALE_MS,
    refetchInterval: false,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })
}

/** Columnar results wire (RQ2-1 §A) for the Results tab. Fetch is gated
 *  on tab visibility (`enabled`) — the ingest walks every task's
 *  results.jsonl, too heavy to pay while the user watches the task list.
 *  Polls while the job is live (results only advance with running tasks),
 *  at a slower cadence than the detail poll. */
export function useJobResultsQuery(
  jobId: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
  active: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => qk.results(toValue(jobId))),
    queryFn: ({ signal }) => jobsApi.results(toValue(jobId), { silent: true, signal }),
    enabled: computed(() => toValue(enabled)),
    refetchInterval: computed(() => (toValue(active) ? 10_000 : false)),
  })
}

/** The experiment's default views (one per section), polled while it runs. */
export function useJobDefaultViewsQuery(
  jobId: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
  active: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => qk.defaultViews(toValue(jobId))),
    queryFn: () => jobsApi.defaultViews(toValue(jobId)),
    enabled: computed(() => toValue(enabled)),
    refetchInterval: computed(() => (toValue(active) ? 30_000 : false)),
  })
}

/** runq.log curves of the cells and the project's pinned curves. */
export function useJobCurvesQuery(
  jobId: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
  active: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => qk.curves(toValue(jobId))),
    queryFn: () => jobsApi.curves(toValue(jobId)),
    enabled: computed(() => toValue(enabled)),
    refetchInterval: computed(() => (toValue(active) ? 30_000 : false)),
  })
}

/** Ranked rows for the leaderboard. Key change re-fetches automatically —
 *  this replaces the metric_keys watch that fired a /compare per poll. */
export function useCompareQuery(
  jobId: MaybeRefOrGetter<string>,
  metricKey: MaybeRefOrGetter<string>,
  desc: MaybeRefOrGetter<boolean>,
  active: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => qk.compare(toValue(jobId), toValue(metricKey), toValue(desc))),
    queryFn: () => jobsApi.compare(toValue(jobId), toValue(metricKey), toValue(desc)),
    enabled: computed(() => !!toValue(metricKey)),
    // metrics only advance while the job runs
    refetchInterval: () => (toValue(active) ? 15_000 : false),
  })
}

/**
 * Job mutations own their cache invalidation — the D-matrix that used to
 * be "every store action remembers to refetch the right lists" by hand.
 */
export function useJobActions() {
  const qc = useQueryClient()
  const config = useConfigStore()
  const { markTasks } = useCancelling()

  const invalidateJob = (id: string) => {
    qc.invalidateQueries({ queryKey: qk.job(id) })
    qc.invalidateQueries({ queryKey: qk.jobs })
  }
  const invalidateArchive = (project?: string) => {
    qc.invalidateQueries({ queryKey: qk.jobs })
    qc.invalidateQueries({ queryKey: qk.jobsArchived })
    if (project) qc.invalidateQueries({ queryKey: qk.projectJobs(project) })
  }

  const kill = useMutation({
    mutationFn: (id: string) => jobsApi.kill(id),
    onSuccess: (_data, id) => {
      const snapshot = qc.getQueryData<JobDetail>(qk.job(id))
      if ((snapshot?.capabilities ?? config.capabilitiesFor(snapshot?.job.target)).kill_async) {
        const detail = qc.getQueryData<JobDetail>(qk.job(id))
        markTasks((detail?.tasks ?? []).filter(t => t.status === 'running').map(t => t.id))
      }
      invalidateJob(id)
    },
  })

  /** Manual reconcile (poll-model): backend re-reads external sources. */
  const refresh = useMutation({
    mutationFn: (id: string) => jobsApi.refresh(id),
    onSuccess: (_d, id) => invalidateJob(id),
  })

  const archive = useMutation({
    mutationFn: ({ id }: { id: string; project?: string }) => jobsApi.archive(id),
    onSuccess: (_d, v) => invalidateArchive(v.project),
  })

  const unarchive = useMutation({
    mutationFn: ({ id }: { id: string; project?: string }) => jobsApi.unarchive(id),
    onSuccess: (_d, v) => invalidateArchive(v.project),
  })

  return { kill, refresh, archive, unarchive, invalidateJob }
}
