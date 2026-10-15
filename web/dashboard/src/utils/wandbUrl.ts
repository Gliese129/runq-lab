/** The job's W&B project page. A run's own URL (from ctx.link_wandb) carries
 *  the real host and entity — https://wandb.ai/<entity>/<project>/runs/<id>
 *  — so the project page is that URL up to the project. The configured
 *  base_url is the fallback; without an entity it resolves against the
 *  viewer's default entity, which is often the wrong one. */
export function wandbProjectUrl(runLinks: readonly (string | undefined)[], baseUrl?: string): { url: string; exact: boolean } | null {
  for (const link of runLinks) {
    if (!link) continue
    try {
      const u = new URL(link)
      const i = u.pathname.indexOf('/runs/')
      if (i > 0) return { url: u.origin + u.pathname.slice(0, i), exact: true }
    } catch { /* not a URL */ }
  }
  if (!baseUrl) return null
  try {
    const segments = new URL(baseUrl).pathname.split('/').filter(Boolean)
    return { url: baseUrl, exact: segments.length >= 2 }
  } catch {
    return null
  }
}
