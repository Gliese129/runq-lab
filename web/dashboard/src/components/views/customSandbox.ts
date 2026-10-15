/** Builds the srcdoc for a custom HTML view. The page runs in an iframe
 *  sandboxed to scripts only (opaque origin: no cookies, no access to the
 *  dashboard or its API) with a CSP that forbids any network request. Data
 *  arrives by postMessage; the template is a Vue template with an optional
 *  <script> (`export default { computed, methods }`) and <style>. */

export interface SplitTemplate {
  markup: string
  script: string
  style: string
}

/** Pull <script> and <style> blocks out of the template file. */
export function splitTemplate(src: string): SplitTemplate {
  let script = ''
  let style = ''
  const markup = src
    .replace(/<script\b[^>]*>([\s\S]*?)<\/script>/gi, (_, body: string) => { script += body; return '' })
    .replace(/<style\b[^>]*>([\s\S]*?)<\/style>/gi, (_, body: string) => { style += body; return '' })
  return { markup: markup.trim(), script: script.trim(), style }
}

/** `export default {…}` → `return {…}` so the block evaluates to options. */
export function scriptBody(script: string): string {
  if (!script) return 'return {}'
  return script.replace(/export\s+default/, 'return')
}

const CSP = "default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:"

const BOOT = String.raw`
const fmt = (v, f) => {
  if (v === null || v === undefined || Number.isNaN(v)) return '--'
  if (typeof v !== 'number') return String(v)
  const m = /^(pct|float|sci)(\d)$/.exec(f || '')
  if (m && m[1] === 'sci') { const [a, e] = v.toExponential(+m[2]).split('e'); return a + 'e' + Number(e) }
  if (m) return m[1] === 'pct' ? (v * 100).toFixed(+m[2]) + '%' : v.toFixed(+m[2])
  if (f === 'int') return Math.round(v).toLocaleString()
  if (f === 'ago') { const s = Math.max(0, Date.now() / 1000 - v); return s < 90 ? Math.round(s) + 's ago' : s < 5400 ? Math.round(s / 60) + 'm ago' : Math.round(s / 3600) + 'h ago' }
  if (f === 'duration') { const s = Math.round(v); return s < 120 ? s + 's' : s < 7200 ? Math.round(s / 60) + 'm' : (s / 3600).toFixed(1) + 'h' }
  return v.toLocaleString('en-US', { useGrouping: false, maximumFractionDigits: 20 })
}
const state = Vue.reactive({ data: {}, rows: [], columns: [], row_keys: [], sources: [], extra: [], now: Date.now() / 1000 })
let options = {}
try { options = (new Function(document.getElementById('user-script').textContent))() || {} }
catch (e) { document.getElementById('err').textContent = 'script: ' + e.message }
const app = Vue.createApp({
  ...options,
  template: document.getElementById('user-template').innerHTML,
  setup() { return { ...Vue.toRefs(state), fmt } },
})
app.config.errorHandler = (e) => { document.getElementById('err').textContent = String(e && e.message || e) }
app.config.warnHandler = () => {}
try { app.mount('#app') } catch (e) { document.getElementById('err').textContent = 'template: ' + e.message }
window.addEventListener('message', (ev) => {
  if (ev.source !== parent || !ev.data || ev.data.type !== 'runq-view') return
  Object.assign(state, ev.data.payload)
})
new ResizeObserver(() => parent.postMessage({ type: 'runq-height', height: document.documentElement.scrollHeight }, '*'))
  .observe(document.body)
parent.postMessage({ type: 'runq-ready' }, '*')
`

function escapeScript(s: string): string {
  return s.replace(/<\/script/gi, '<\\/script')
}

export function buildSrcdoc(template: string, vueSource: string): string {
  const { markup, script, style } = splitTemplate(template)
  return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${CSP}">
<style>body{margin:0;padding:8px;font-family:Helvetica,Arial,sans-serif;color:#1a1a1a;background:#fff}#err{color:#b42318;font:12px monospace;white-space:pre-wrap}${style}</style>
</head><body><div id="err"></div><div id="app"></div>
<template id="user-template">${markup}</template>
<script id="user-script" type="text/plain">${escapeScript(scriptBody(script))}</script>
<script>${escapeScript(vueSource)}</script>
<script>${escapeScript(BOOT)}</script>
</body></html>`
}
