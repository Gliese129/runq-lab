import { describe, expect, it } from 'vitest'
import { buildSrcdoc, scriptBody, splitTemplate } from './customSandbox'

describe('custom view sandbox', () => {
  it('splits markup, script and style', () => {
    const t = splitTemplate('<p>{{ x }}</p>\n<script>export default { computed: { x() { return 1 } } }</script><style>p{color:red}</style>')
    expect(t.markup).toBe('<p>{{ x }}</p>')
    expect(scriptBody(t.script)).toBe('return { computed: { x() { return 1 } } }')
    expect(t.style).toBe('p{color:red}')
  })
  it('forbids network access and cannot be broken out of by a script tag', () => {
    const doc = buildSrcdoc('<p>hi</p><script>const s = "</script><b>x</b>"</script>', '/* vue */')
    expect(doc).toContain("default-src 'none'")
    expect(doc).not.toContain('"</script><b>')
  })
})
