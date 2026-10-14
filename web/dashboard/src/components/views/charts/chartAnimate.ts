/** Animation export: frames are canvases rendered from the chart's SVG at
 *  each history point; GIF via gifenc (loaded on demand), WebM via the
 *  browser's MediaRecorder. For reports and your own reading, not papers. */

/** Corner stamp so a frame says when (or at which step) it is. */
export function stamp(canvas: HTMLCanvasElement, text: string) {
  const ctx = canvas.getContext('2d')!
  const size = Math.round(canvas.width / 40)
  ctx.font = `${size}px Helvetica, Arial, sans-serif`
  ctx.fillStyle = '#666'
  ctx.textAlign = 'right'
  ctx.fillText(text, canvas.width - size * 0.6, canvas.height - size * 0.5)
}

export async function encodeGif(frames: HTMLCanvasElement[], delayMs: number): Promise<Blob> {
  const { GIFEncoder, quantize, applyPalette } = await import('gifenc')
  const gif = GIFEncoder()
  frames.forEach((c, i) => {
    const { data, width, height } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height)
    const palette = quantize(data, 256)
    // Hold the final frame so the end state can be read.
    gif.writeFrame(applyPalette(data, palette), width, height, { palette, delay: i === frames.length - 1 ? delayMs * 6 : delayMs })
  })
  gif.finish()
  return new Blob([gif.bytes()], { type: 'image/gif' })
}

export async function encodeWebm(frames: HTMLCanvasElement[], fps: number): Promise<Blob> {
  if (!frames.length) throw new Error('no frames')
  const out = document.createElement('canvas')
  out.width = frames[0].width
  out.height = frames[0].height
  const ctx = out.getContext('2d')!
  const stream = out.captureStream(fps)
  const type = ['video/webm;codecs=vp9', 'video/webm'].find((t) => MediaRecorder.isTypeSupported(t))
  if (!type) throw new Error('this browser cannot record WebM')
  const rec = new MediaRecorder(stream, { mimeType: type, videoBitsPerSecond: 4_000_000 })
  const chunks: Blob[] = []
  rec.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data) }
  const done = new Promise<void>((ok) => { rec.onstop = () => ok() })
  rec.start()
  const hold = [...frames, ...Array(Math.round(fps * 1.5)).fill(frames[frames.length - 1])]
  for (const f of hold) {
    ctx.drawImage(f, 0, 0)
    await new Promise((r) => setTimeout(r, 1000 / fps))
  }
  rec.stop()
  await done
  return new Blob(chunks, { type: 'video/webm' })
}
