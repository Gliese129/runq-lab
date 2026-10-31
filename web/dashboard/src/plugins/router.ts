import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('../layouts/DefaultLayout.vue'),
    children: [
      { path: '', name: 'overview', component: () => import('../views/Overview.vue'), meta: { wide: true } },
      { path: 'drafts/:draftId', name: 'draft', component: () => import('../views/DraftPage.vue'), props: true, meta: { wide: true } },
      { path: 'projects/:project', name: 'project', component: () => import('../views/ProjectJobs.vue'), props: true, meta: { wide: true } },
      { path: 'projects/:project/views/:name', name: 'view', component: () => import('../views/ViewPage.vue'), props: true, meta: { wide: true } },
      { path: 'projects/:project/:jobId', name: 'job-detail', component: () => import('../views/job/index.vue'), props: true },
      { path: 'projects/:project/:jobId/:taskId', name: 'task-detail', component: () => import('../views/job/TaskDetail.vue'), props: true },
      { path: 'targets', name: 'targets', component: () => import('../views/Targets.vue') },
      { path: 'settings', name: 'settings', component: () => import('../views/Settings.vue') },
      { path: 'about', name: 'about', component: () => import('../views/About.vue') },
    ],
  },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

// A page opened before runq was upgraded still names the old page chunks;
// once they are gone, the next navigation would fail silently. Reload into
// the new build instead (once per minute, so a real outage cannot loop).
const RELOADED_AT = 'runq-chunk-reload'
function reloadIntoNewBuild(to?: string) {
  let last = 0
  try { last = Number(sessionStorage.getItem(RELOADED_AT) ?? 0) } catch { /* private mode */ }
  if (Date.now() - last < 60_000) return
  try { sessionStorage.setItem(RELOADED_AT, String(Date.now())) } catch { /* private mode */ }
  if (to) window.location.hash = to
  window.location.reload()
}
router.onError((err, to) => {
  if (/dynamically imported module|Importing a module script failed|Failed to fetch/i.test(String(err?.message ?? err))) {
    reloadIntoNewBuild(to.fullPath)
  }
})
window.addEventListener('vite:preloadError', () => reloadIntoNewBuild())

export default router
