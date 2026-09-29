import DOMPurify from 'dompurify'
import mammoth from 'mammoth'
import * as XLSX from 'xlsx'
import { unzipSync } from 'fflate'

type OfficeKind = 'word' | 'excel' | 'powerpoint'

// OCTO-FORK: Office previews are built in the browser from the authenticated
// artifact bytes; no external viewer or upload is needed.
export function officeKindForPath(path: string): OfficeKind | null {
  const ext = path.slice(path.lastIndexOf('.') + 1).toLowerCase()
  if (ext === 'docx') return 'word'
  if (ext === 'xlsx') return 'excel'
  if (ext === 'pptx') return 'powerpoint'
  return null
}

export async function renderOfficePreview(
  name: string,
  bytes: ArrayBuffer,
  dark: boolean,
): Promise<string> {
  const kind = officeKindForPath(name)
  if (!kind) throw new Error('This Office format is not supported in the browser preview.')

  let body: string
  switch (kind) {
    case 'word': {
      const result = await mammoth.convertToHtml({ arrayBuffer: bytes }, {
        includeDefaultStyleMap: true,
        styleMap: [
          "p[style-name='Title'] => h1.document-title:fresh",
          "p[style-name='Subtitle'] => p.document-subtitle:fresh",
          "p[style-name='Quote'] => blockquote:fresh",
          "p[style-name='Intense Quote'] => blockquote:fresh",
          "p[style-name='Caption'] => figcaption:fresh",
        ],
        convertImage: mammoth.images.dataUri,
      })
      body = `<article class="word-document">${result.value}</article>`
      break
    }
    case 'excel': {
      const workbook = XLSX.read(bytes, { type: 'array' })
      body = workbook.SheetNames.map((sheetName) => {
        const sheet = workbook.Sheets[sheetName]
        const table = stripDocumentShell(XLSX.utils.sheet_to_html(sheet, { editable: false }))
        return `<section class="sheet"><div class="sheet-heading"><span class="sheet-kicker">Worksheet</span><h2>${escapeHTML(sheetName)}</h2></div>${table}</section>`
      }).join('')
      break
    }
    case 'powerpoint':
      body = renderPowerPoint(bytes)
      break
  }

  const clean = DOMPurify.sanitize(body, {
    USE_PROFILES: { html: true, svg: true, svgFilters: true },
    // Keep generated classes so the wrapper can style sheets/slides while
    // still dropping inline styles and ids from document content.
    FORBID_ATTR: ['style', 'id'],
  })
  return officeDocument(clean, kind, dark)
}

function officeDocument(body: string, kind: OfficeKind, dark: boolean): string {
  const colors = dark
    ? { bg: '#1e1e1e', text: '#d4d4d4', muted: '#a6a6a6', border: '#3a3a3a', paper: '#252525', accent: '#78a9ff' }
    : { bg: '#f5f7fa', text: '#1f2329', muted: '#667085', border: '#d9dee8', paper: '#ffffff', accent: '#1677ff' }
  const base = `<style>
    :root { color-scheme: ${dark ? 'dark' : 'light'}; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      padding: clamp(12px, 3vw, 28px);
      background: ${colors.bg};
      color: ${colors.text};
      font: 14px/1.65 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      -webkit-font-smoothing: antialiased;
    }
    main { width: min(100%, 1040px); margin: 0 auto; }
    .word-document {
      width: min(100%, 840px);
      min-height: 820px;
      margin: 0 auto;
      padding: clamp(30px, 6vw, 72px) clamp(24px, 7vw, 78px);
      background: ${colors.paper};
      border: 1px solid ${colors.border};
      border-radius: 14px;
      box-shadow: 0 12px 36px rgba(15, 23, 42, ${dark ? '.18' : '.08'});
      line-height: 1.75;
    }
    .word-document h1, .word-document h2, .word-document h3, .word-document h4 {
      margin: 0 0 18px;
      color: ${colors.text};
      line-height: 1.3;
      letter-spacing: -.01em;
    }
    .word-document h1 { font-size: clamp(26px, 4vw, 34px); }
    .word-document h2 { margin-top: 34px; font-size: 23px; }
    .word-document h3 { margin-top: 26px; font-size: 18px; }
    .word-document p { max-width: 72ch; margin: 0 0 14px; }
    .word-document ul, .word-document ol { margin: 0 0 16px; padding-left: 28px; }
    .word-document li { margin: 5px 0; }
    .word-document blockquote {
      margin: 22px 0;
      padding: 14px 18px;
      border-left: 4px solid ${colors.accent};
      border-radius: 0 8px 8px 0;
      background: ${dark ? '#2d2d2d' : '#f5f8ff'};
      color: ${colors.muted};
    }
    .word-document table { width: 100%; margin: 22px 0; border-collapse: separate; border-spacing: 0; font-size: 13px; overflow: hidden; }
    .word-document th, .word-document td { padding: 9px 11px; border-right: 1px solid ${colors.border}; border-bottom: 1px solid ${colors.border}; text-align: left; vertical-align: top; }
    .word-document th:first-child, .word-document td:first-child { border-left: 1px solid ${colors.border}; }
    .word-document thead th { border-top: 1px solid ${colors.border}; background: ${dark ? '#303030' : '#f0f4fa'}; font-weight: 650; }
    .word-document thead th:first-child { border-radius: 8px 0 0 0; }
    .word-document thead th:last-child { border-radius: 0 8px 0 0; }
    .word-document tbody tr:nth-child(even) td { background: ${dark ? '#282828' : '#fafbfc'}; }
    .word-document img { display: block; max-width: 100%; height: auto; margin: 22px auto; border-radius: 8px; }
    .word-document figcaption { margin: -10px 0 18px; color: ${colors.muted}; font-size: 12px; text-align: center; }
    .word-document a { color: ${colors.accent}; text-underline-offset: 3px; }
    .document-title { margin-bottom: 10px !important; text-align: center; }
    .document-subtitle { margin-bottom: 30px !important; color: ${colors.muted}; text-align: center; }
    .sheet {
      margin: 0 0 18px;
      overflow: auto;
      background: ${colors.paper};
      border: 1px solid ${colors.border};
      border-radius: 12px;
      box-shadow: 0 8px 24px rgba(15, 23, 42, ${dark ? '.16' : '.06'});
      overscroll-behavior-x: contain;
    }
    .sheet-heading { display: flex; align-items: baseline; gap: 10px; padding: 17px 20px 14px; border-bottom: 1px solid ${colors.border}; }
    .sheet-kicker { color: ${colors.muted}; font-size: 11px; font-weight: 600; letter-spacing: .08em; text-transform: uppercase; }
    .sheet h2 { margin: 0; color: ${colors.accent}; font-size: 15px; font-weight: 650; }
    .sheet table { width: max(100%, 620px); border-collapse: separate; border-spacing: 0; font-size: 13px; }
    .sheet th, .sheet td { padding: 9px 11px; border-right: 1px solid ${colors.border}; border-bottom: 1px solid ${colors.border}; text-align: left; vertical-align: top; }
    .sheet th:first-child, .sheet td:first-child { border-left: 1px solid ${colors.border}; }
    .sheet thead th { position: sticky; top: 0; z-index: 1; border-top: 1px solid ${colors.border}; background: ${dark ? '#303030' : '#f0f4fa'}; color: ${colors.text}; font-weight: 650; box-shadow: 0 1px 0 ${colors.border}; }
    .sheet table > tbody > tr:first-child td { position: sticky; top: 0; z-index: 1; background: ${dark ? '#303030' : '#f0f4fa'}; color: ${colors.text}; font-weight: 650; box-shadow: 0 1px 0 ${colors.border}; }
    .sheet tbody tr:nth-child(even) td { background: ${dark ? '#282828' : '#fafbfc'}; }
    .sheet tbody tr:hover td { background: ${dark ? '#333333' : '#f3f7ff'}; }
    .ppt-slide { margin: 0 0 22px; padding: 0; }
    .ppt-slide-number { display: inline-flex; margin: 0 0 9px 2px; color: ${colors.muted}; font-size: 11px; font-weight: 600; letter-spacing: .04em; }
    .ppt-canvas { display: block; width: 100%; height: auto; aspect-ratio: 16 / 9; background: ${colors.paper}; border: 1px solid ${colors.border}; border-radius: 12px; box-shadow: 0 12px 30px rgba(15, 23, 42, ${dark ? '.2' : '.08'}); }
    .ppt-slide p { margin: 0 0 14px; font-size: 20px; line-height: 1.45; }
    .ppt-image { display: block; max-width: 100%; max-height: 520px; height: auto; margin: 18px auto; object-fit: contain; }
    .empty-slide { color: ${colors.muted}; font-style: italic; }
    @media (max-width: 640px) {
      .word-document { min-height: 0; border-radius: 10px; }
      .sheet-heading { padding: 14px 16px 12px; }
      .ppt-canvas { border-radius: 8px; }
    }
    @media (prefers-reduced-motion: reduce) {
      *, *::before, *::after { scroll-behavior: auto !important; transition: none !important; }
    }
  </style>`
  return `${base}<main data-office-kind="${kind}">${body || '<div class="empty-slide">No readable content found.</div>'}</main>`
}

function renderPowerPoint(bytes: ArrayBuffer): string {
  const files = unzipSync(new Uint8Array(bytes))
  const slideSize = presentationSlideSize(files)
  const slides = Object.keys(files)
    .filter((name) => /^ppt\/slides\/slide\d+\.xml$/.test(name))
    .sort((a, b) => slideNumber(a) - slideNumber(b))

  return slides.map((name, index) => {
    const xml = new TextDecoder().decode(files[name])
    const doc = new DOMParser().parseFromString(xml, 'application/xml')
    const visual = renderPowerPointVisual(name, doc, files, slideSize)
    const paragraphs = Array.from(doc.getElementsByTagNameNS('*', 'p')).map((paragraph) =>
      Array.from(paragraph.getElementsByTagNameNS('*', 't'))
        .map((text) => text.textContent ?? '')
        .join('')
        .trim(),
    ).filter(Boolean)
    const content = paragraphs.length
      ? paragraphs.map((text) => `<p>${escapeHTML(text)}</p>`).join('')
      : '<div class="empty-slide">No readable text on this slide.</div>'
    return `<section class="ppt-slide"><div class="ppt-slide-number">Slide ${index + 1} / ${slides.length}</div>${visual || content}</section>`
  }).join('')
}

function presentationSlideSize(files: Record<string, Uint8Array>): { width: number, height: number } {
  const xml = files['ppt/presentation.xml']
  if (!xml) return { width: 1280, height: 720 }
  const doc = new DOMParser().parseFromString(new TextDecoder().decode(xml), 'application/xml')
  const size = doc.getElementsByTagNameNS('*', 'sldSz')[0]
  return {
    width: Math.max(1, Number(size?.getAttribute('cx') ?? 12192000)),
    height: Math.max(1, Number(size?.getAttribute('cy') ?? 6858000)),
  }
}

function renderPowerPointVisual(
  slidePath: string,
  doc: Document,
  files: Record<string, Uint8Array>,
  slideSize: { width: number, height: number },
): string {
  const width = 1280
  const height = Math.round(width * slideSize.height / slideSize.width)
  const parts: string[] = []
  for (const shape of Array.from(doc.getElementsByTagNameNS('*', 'sp'))) {
    const transform = shapeTransform(shape)
    if (!transform) continue
    const x = transform.x / slideSize.width * width
    const y = transform.y / slideSize.height * height
    const w = transform.width / slideSize.width * width
    const h = transform.height / slideSize.height * height
    const fill = shapeFill(shape)
    const geometry = shape.getElementsByTagNameNS('*', 'prstGeom')[0]?.getAttribute('prst')
    if (geometry === 'ellipse') {
      parts.push(`<ellipse cx="${x + w / 2}" cy="${y + h / 2}" rx="${w / 2}" ry="${h / 2}" fill="${fill}" />`)
    } else if (geometry === 'line') {
      parts.push(`<line x1="${x}" y1="${y}" x2="${x + w}" y2="${y + h}" stroke="${shapeLineColor(shape)}" stroke-width="2" />`)
    } else {
      const radius = geometry === 'roundRect' ? Math.min(w, h) * 0.08 : 0
      parts.push(`<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${radius}" fill="${fill}" />`)
    }
    parts.push(shapeText(shape, x, y, w, h))
  }
  for (const image of slideImageRefs(slidePath, doc, files)) {
    const transform = image.transform
    if (!transform) continue
    const file = files[image.path]
    const mime = imageMime(image.path)
    if (!file || !mime) continue
    const x = transform.x / slideSize.width * width
    const y = transform.y / slideSize.height * height
    const w = transform.width / slideSize.width * width
    const h = transform.height / slideSize.height * height
    parts.push(`<image class="ppt-image" x="${x}" y="${y}" width="${w}" height="${h}" preserveAspectRatio="none" href="data:${mime};base64,${toBase64(file)}" />`)
  }
  if (!parts.length) return ''
  return `<svg class="ppt-canvas" viewBox="0 0 ${width} ${height}" role="img" aria-label="PowerPoint slide">${parts.join('')}</svg>`
}

function slideImageRefs(slidePath: string, doc: Document, files: Record<string, Uint8Array>): Array<{ path: string, transform: Transform | null }> {
  const relsPath = `${slidePath.slice(0, slidePath.lastIndexOf('/'))}/_rels/${slidePath.slice(slidePath.lastIndexOf('/') + 1)}.rels`
  const rels = files[relsPath]
  if (!rels) return []
  const relDoc = new DOMParser().parseFromString(new TextDecoder().decode(rels), 'application/xml')
  const targets = new Map<string, string>()
  for (const relation of Array.from(relDoc.getElementsByTagNameNS('*', 'Relationship'))) {
    const id = relation.getAttribute('Id')
    const target = relation.getAttribute('Target')
    const mode = relation.getAttribute('TargetMode')
    if (id && target && mode !== 'External') targets.set(id, resolveZipPath(slidePath, target))
  }
  return Array.from(doc.getElementsByTagNameNS('*', 'pic')).map((picture) => {
    const blip = picture.getElementsByTagNameNS('*', 'blip')[0]
    const relId = blip?.getAttributeNS('http://schemas.openxmlformats.org/officeDocument/2006/relationships', 'embed')
      || blip?.getAttribute('r:embed')
    const path = relId ? targets.get(relId) : undefined
    return path && files[path] && imageMime(path) ? { path, transform: shapeTransform(picture) } : null
  }).filter((image): image is { path: string, transform: Transform | null } => !!image)
}

function shapeTransform(element: Element): Transform | null {
  const xfrm = element.getElementsByTagNameNS('*', 'xfrm')[0]
  const off = xfrm?.getElementsByTagNameNS('*', 'off')[0]
  const ext = xfrm?.getElementsByTagNameNS('*', 'ext')[0]
  if (!off || !ext) return null
  const values = {
    x: Number(off.getAttribute('x')),
    y: Number(off.getAttribute('y')),
    width: Number(ext.getAttribute('cx')),
    height: Number(ext.getAttribute('cy')),
  }
  return Object.values(values).every(Number.isFinite) ? values : null
}

type Transform = { x: number, y: number, width: number, height: number }

function shapeFill(shape: Element): string {
  const spPr = shape.getElementsByTagNameNS('*', 'spPr')[0]
  const color = spPr?.getElementsByTagNameNS('*', 'solidFill')[0]?.getElementsByTagNameNS('*', 'srgbClr')[0]?.getAttribute('val')
  return color && /^[0-9a-f]{6}$/i.test(color) ? `#${color}` : 'none'
}

function shapeLineColor(shape: Element): string {
  const spPr = shape.getElementsByTagNameNS('*', 'spPr')[0]
  const color = spPr?.getElementsByTagNameNS('*', 'ln')[0]?.getElementsByTagNameNS('*', 'srgbClr')[0]?.getAttribute('val')
  return color && /^[0-9a-f]{6}$/i.test(color) ? `#${color}` : '#64748b'
}

function shapeText(shape: Element, x: number, y: number, width: number, height: number): string {
  const txBody = shape.getElementsByTagNameNS('*', 'txBody')[0]
  if (!txBody) return ''
  const paragraphs = Array.from(txBody.getElementsByTagNameNS('*', 'p'))
  const bodyPr = txBody.getElementsByTagNameNS('*', 'bodyPr')[0]
  const maxWidth = Math.max(16, width - 16)
  const textLines: Array<{ text: string, size: number, fill: string, bold: boolean, italic: boolean, align: string }> = []
  paragraphs.forEach((paragraph) => {
    const text = Array.from(paragraph.getElementsByTagNameNS('*', 't')).map((node) => node.textContent ?? '').join('')
    if (!text) return
    const runProperties = paragraph.getElementsByTagNameNS('*', 'rPr')[0]
    const size = Math.max(8, Number(runProperties?.getAttribute('sz') ?? 1400) / 100)
    const bold = runProperties?.getAttribute('b') === '1'
    const italic = runProperties?.getAttribute('i') === '1'
    const color = runProperties?.getElementsByTagNameNS('*', 'srgbClr')[0]?.getAttribute('val')
    const fill = color && /^[0-9a-f]{6}$/i.test(color) ? `#${color}` : '#1f2937'
    const align = paragraph.getElementsByTagNameNS('*', 'pPr')[0]?.getAttribute('algn')
    for (const line of wrapSvgText(text, size, maxWidth)) {
      textLines.push({ text: line, size, fill, bold, italic, align: align === 'ctr' ? 'middle' : align === 'r' ? 'end' : 'start' })
    }
  })
  if (!textLines.length) return ''

  const lineHeight = Math.max(...textLines.map((line) => line.size * 1.2))
  const totalHeight = textLines.length * lineHeight
  const scale = totalHeight > height - 8 ? Math.max(0.6, (height - 8) / totalHeight) : 1
  const verticalAnchor = bodyPr?.getAttribute('anchor')
  const firstY = verticalAnchor === 'b'
    ? y + height - totalHeight * scale + lineHeight * scale * 0.85
    : verticalAnchor === 'ctr'
      ? y + (height - totalHeight * scale) / 2 + lineHeight * scale * 0.85
      : y + lineHeight * scale * 0.85

  return textLines.map((line, index) => {
    const textX = line.align === 'middle' ? x + width / 2 : line.align === 'end' ? x + width - 8 : x + 8
    const textY = firstY + index * lineHeight * scale
    const fontSize = line.size * scale
    const weight = line.bold ? ' font-weight="700"' : ''
    const style = line.italic ? ' font-style="italic"' : ''
    return `<text x="${textX}" y="${textY}" fill="${line.fill}" font-size="${fontSize}" text-anchor="${line.align}"${weight}${style}>${escapeHTML(line.text)}</text>`
  }).join('')
}

function wrapSvgText(value: string, fontSize: number, maxWidth: number): string[] {
  const lines: string[] = []
  let current = ''
  let currentWidth = 0
  for (const char of value) {
    if (char === '\n') {
      lines.push(current)
      current = ''
      currentWidth = 0
      continue
    }
    const charWidth = /[\u0000-\u00ff]/.test(char) ? fontSize * 0.58 : fontSize
    if (current && currentWidth + charWidth > maxWidth) {
      lines.push(current)
      current = ''
      currentWidth = 0
    }
    current += char
    currentWidth += charWidth
  }
  if (current || !lines.length) lines.push(current)
  return lines
}

function resolveZipPath(sourcePath: string, target: string): string {
  const base = sourcePath.slice(0, sourcePath.lastIndexOf('/')).split('/')
  const parts = [...base, ...target.split('/')]
  const normalized: string[] = []
  for (const part of parts) {
    if (!part || part === '.') continue
    if (part === '..') normalized.pop()
    else normalized.push(part)
  }
  return normalized.join('/')
}

function imageMime(path: string): string | null {
  const ext = path.slice(path.lastIndexOf('.') + 1).toLowerCase()
  return ({ png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', svg: 'image/svg+xml', bmp: 'image/bmp', tif: 'image/tiff', tiff: 'image/tiff' } as Record<string, string>)[ext] ?? null
}

function toBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000))
  }
  return btoa(binary)
}

function slideNumber(path: string): number {
  return Number(path.match(/slide(\d+)\.xml$/)?.[1] ?? 0)
}

function stripDocumentShell(html: string): string {
  return html
    .replace(/^\s*<html[^>]*>\s*<head>[\s\S]*?<\/head>\s*<body[^>]*>/i, '')
    .replace(/<\/body>\s*<\/html>\s*$/i, '')
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!)
}
