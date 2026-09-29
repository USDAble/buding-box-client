import { describe, expect, it } from 'vitest'
import { strToU8, zipSync } from 'fflate'
import * as XLSX from 'xlsx'
import { officeKindForPath, renderOfficePreview } from './office-preview'

describe('office previews', () => {
  it('maps only modern Office formats to browser previews', () => {
    expect(officeKindForPath('report.docx')).toBe('word')
    expect(officeKindForPath('table.xlsx')).toBe('excel')
    expect(officeKindForPath('slides.pptx')).toBe('powerpoint')
    expect(officeKindForPath('old.doc')).toBeNull()
  })

  it('renders PowerPoint slide text without executing document content', async () => {
    const xml = `<?xml version="1.0"?><p:sld xmlns:p="urn:p" xmlns:a="urn:a" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:cSld><p:sp><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="100" cy="100"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:p><a:r><a:t>Quarterly plan</a:t></a:r></a:p></p:txBody></p:sp><p:pic><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="100" cy="100"/></a:xfrm></p:spPr><p:blipFill><a:blip r:embed="rId1"/></p:blipFill></p:pic></p:cSld></p:sld>`
    const rels = `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="../media/image1.png" TargetMode="Internal"/></Relationships>`
    const zipped = zipSync({
      'ppt/slides/slide1.xml': strToU8(xml),
      'ppt/slides/_rels/slide1.xml.rels': strToU8(rels),
      'ppt/media/image1.png': strToU8('fake-png'),
    })
    const preview = await renderOfficePreview('slides.pptx', zipped.buffer, false)
    expect(preview).toContain('Quarterly')
    expect(preview).toContain('plan')
    expect(preview).toContain('data-office-kind="powerpoint"')
    expect(preview).toContain('class="ppt-slide"')
    expect(preview).toContain('class="ppt-image"')
    expect(preview).toContain('data:image/png;base64,ZmFrZS1wbmc=')
    expect(preview).not.toContain('<script')
  })

  it('renders Excel sheet tables', async () => {
    const workbook = XLSX.utils.book_new()
    const sheet = XLSX.utils.aoa_to_sheet([
      ['Quarter', 'Revenue'],
      ['Q1', 120],
    ])
    XLSX.utils.book_append_sheet(workbook, sheet, 'Summary')
    const bytes = XLSX.write(workbook, { bookType: 'xlsx', type: 'array' })
    const preview = await renderOfficePreview('table.xlsx', bytes, false)

    expect(preview).toContain('Summary')
    expect(preview).toContain('Revenue')
    expect(preview).toContain('120')
  })
})
