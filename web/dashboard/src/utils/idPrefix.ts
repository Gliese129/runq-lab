/** Tables show ids shortened to 8 characters (`jb657361`); people copy
 *  those into URLs. Resolve a prefix against the known ids the way the
 *  CLI does: exactly one match → that id; otherwise the candidates. */
export function resolveIdPrefix(prefix: string, ids: readonly string[]): { match?: string; candidates: string[] } {
  if (!prefix) return { candidates: [] }
  const candidates = ids.filter((id) => id.startsWith(prefix) && id !== prefix)
  return candidates.length === 1 ? { match: candidates[0], candidates } : { candidates }
}
