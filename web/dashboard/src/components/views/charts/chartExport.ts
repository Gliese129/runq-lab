/** Figure export: the on-screen SVG is the figure, so every format starts
 *  from its serialized markup (colours and fonts are inline, never CSS). */

// Vue's scoped-style data attributes are noise in a figure file.
function strip(n: Element) {
  // Copy first: attributes is live and shrinks as we remove.
  for (const a of Array.from(n.attributes)) if (a.name.startsWith('data-v-')) n.removeAttribute(a.name)
}

export function svgString(svg: SVGSVGElement): string {
  const clone = svg.cloneNode(true) as SVGSVGElement
  clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg')
  strip(clone)
  for (const n of clone.querySelectorAll('*')) strip(n)
  return '<?xml version="1.0" encoding="UTF-8"?>\n' + new XMLSerializer().serializeToString(clone)
}

export function downloadBlob(blob: Blob, name: string) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}

/** Rasterize at `dpi`, the figure's px being 1/100 in. */
export async function svgToCanvas(svg: SVGSVGElement, dpi = 300): Promise<HTMLCanvasElement> {
  const w = Number(svg.getAttribute('width')), h = Number(svg.getAttribute('height'))
  const scale = dpi / 100
  const img = new Image()
  const url = URL.createObjectURL(new Blob([svgString(svg)], { type: 'image/svg+xml' }))
  try {
    await new Promise<void>((ok, fail) => { img.onload = () => ok(); img.onerror = () => fail(new Error('could not rasterize the figure')); img.src = url })
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(w * scale)
    canvas.height = Math.round(h * scale)
    const ctx = canvas.getContext('2d')!
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, canvas.width, canvas.height)
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
    return canvas
  } finally {
    URL.revokeObjectURL(url)
  }
}

export async function svgToPng(svg: SVGSVGElement, dpi = 300): Promise<Blob> {
  const canvas = await svgToCanvas(svg, dpi)
  return await new Promise<Blob>((ok, fail) => canvas.toBlob((b) => (b ? ok(b) : fail(new Error('PNG encoding failed'))), 'image/png'))
}

/** Vector PDF at the figure's physical size (px = 1/100 in → 0.72 pt).
 *  jsPDF + svg2pdf load only when someone exports a PDF. */
export async function svgToPdf(svg: SVGSVGElement): Promise<Blob> {
  const [{ jsPDF }, { svg2pdf }] = await Promise.all([import('jspdf'), import('svg2pdf.js')])
  const w = Number(svg.getAttribute('width')) * 0.72, h = Number(svg.getAttribute('height')) * 0.72
  const doc = new jsPDF({ unit: 'pt', format: [w, h], orientation: w > h ? 'landscape' : 'portrait' })
  const clone = new DOMParser().parseFromString(svgString(svg), 'image/svg+xml').documentElement as unknown as SVGSVGElement
  await svg2pdf(clone, doc, { x: 0, y: 0, width: w, height: h })
  return doc.output('blob')
}
