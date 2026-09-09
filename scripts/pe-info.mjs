// Reads the two Windows PE fields the portable self-check needs — the subsystem
// (GUI vs console) and the VERSIONINFO string table — with no third-party
// dependency (see P12-便携打包.md §3.3). A hand-rolled reader rather than a
// package keeps the packaging scripts zero-dependency, matching the other
// scripts/ tools (sync-branding, datapath-guard).
//
// The reader only descends far enough to reach RT_VERSION's StringFileInfo;
// everything else in the resource tree is skipped. Any malformed input throws
// rather than guessing, so a failed read fails the self-check loudly.

function u16(buf, off) {
  if (off < 0 || off + 2 > buf.length) throw new Error(`PE: read past end at 0x${off.toString(16)}`)
  return buf.readUInt16LE(off)
}

function u32(buf, off) {
  if (off < 0 || off + 4 > buf.length) throw new Error(`PE: read past end at 0x${off.toString(16)}`)
  return buf.readUInt32LE(off)
}

function align4(n) {
  return (n + 3) & ~3
}

// readUtf16 decodes a null-terminated UTF-16LE string starting at off. The
// terminator is not included in the result; off is advanced by callers.
function readUtf16(buf, off) {
  let end = off
  while (end + 1 < buf.length && !(buf[end] === 0 && buf[end + 1] === 0)) end += 2
  if (end + 1 >= buf.length) throw new Error('PE: unterminated UTF-16 key in version info')
  return buf.toString('utf16le', off, end)
}

// parseHeaders locates the PE header and returns the optional-header start,
// the magic (PE32 vs PE32+), and the section table.
function parseHeaders(buf) {
  if (buf.length < 0x40) throw new Error('PE: file too short for a DOS header')
  if (buf.readUInt16LE(0) !== 0x5a4d) throw new Error('PE: missing MZ signature')
  const e = u32(buf, 0x3c)
  if (e + 24 > buf.length) throw new Error('PE: e_lfanew points past end of file')
  if (buf.toString('ascii', e, e + 4) !== 'PE\0\0') throw new Error('PE: missing PE signature')

  const numSections = u16(buf, e + 4 + 2)
  const sizeOfOptionalHeader = u16(buf, e + 4 + 16)
  const optStart = e + 4 + 20
  const magic = u16(buf, optStart)
  if (magic !== 0x10b && magic !== 0x20b) {
    throw new Error(`PE: unsupported optional-header magic 0x${magic.toString(16)}`)
  }

  const sections = []
  const sectionStart = optStart + sizeOfOptionalHeader
  for (let i = 0; i < numSections; i++) {
    const s = sectionStart + i * 40
    const virtualSize = u32(buf, s + 8)
    const virtualAddress = u32(buf, s + 12)
    const sizeOfRawData = u32(buf, s + 16)
    const pointerToRawData = u32(buf, s + 20)
    sections.push({
      name: buf.toString('ascii', s, s + 8).replace(/\0.*$/, ''),
      virtualSize,
      virtualAddress,
      sizeOfRawData,
      pointerToRawData,
    })
  }
  return { e, magic, optStart, sections }
}

// subsystem returns the PE's Subsystem field (2 = Windows GUI, 3 = console).
export function subsystem(buf) {
  const { optStart } = parseHeaders(buf)
  return u16(buf, optStart + 68)
}

function sectionForRva(sections, rva) {
  for (const s of sections) {
    const start = s.virtualAddress
    const end = start + Math.max(s.virtualSize, s.sizeOfRawData)
    if (rva >= start && rva < end) return s
  }
  return null
}

function rvaToOffset(sections, rva) {
  const s = sectionForRva(sections, rva)
  if (!s) throw new Error(`PE: RVA 0x${rva.toString(16)} is outside every section`)
  return s.pointerToRawData + (rva - s.virtualAddress)
}

// dirEntries reads the (name, offset) entries of an IMAGE_RESOURCE_DIRECTORY
// at the given file offset. The high bit of name marks a string name (vs id),
// and the high bit of the offset marks a subdirectory (vs a data RVA) — the
// raw values are returned so callers can branch.
function dirEntries(buf, offset) {
  const count = u16(buf, offset + 12) + u16(buf, offset + 14)
  const entries = []
  for (let i = 0; i < count; i++) {
    const e = offset + 16 + i * 8
    entries.push({ name: u32(buf, e), offToData: u32(buf, e + 4) })
  }
  return entries
}

// findVersionData walks type 16 (RT_VERSION) → first name/id → first language
// and returns the file offset + size of the VS_VERSION_INFO blob.
function findVersionData(buf, sections) {
  const rsrc = sections.find((s) => s.name === '.rsrc')
  if (!rsrc) throw new Error('PE: no .rsrc section')

  const rootOffset = rsrc.pointerToRawData
  const typeEntry = dirEntries(buf, rootOffset).find(
    (en) => (en.name & 0x80000000) === 0 && en.name === 16,
  )
  if (!typeEntry) throw new Error('PE: no RT_VERSION resource')
  if ((typeEntry.offToData & 0x80000000) === 0) throw new Error('PE: RT_VERSION entry is not a directory')

  const nameDir = rootOffset + (typeEntry.offToData & 0x7fffffff)
  const nameEntry = dirEntries(buf, nameDir)[0]
  if (!nameEntry) throw new Error('PE: empty version name directory')
  if ((nameEntry.offToData & 0x80000000) === 0) throw new Error('PE: version name entry is not a directory')

  const langDir = rootOffset + (nameEntry.offToData & 0x7fffffff)
  const langEntry = dirEntries(buf, langDir)[0]
  if (!langEntry) throw new Error('PE: empty version language directory')
  if ((langEntry.offToData & 0x80000000) !== 0) throw new Error('PE: version language entry is a directory')

  // winres (generate-syso) emits the leaf directory entry's offset relative to
  // the start of the .rsrc section, not as an image RVA, when the exe is linked
  // by Go's internal linker (CGO_ENABLED=0, which is how the desktop exe builds
  // on Windows). The IMAGE_RESOURCE_DATA_ENTRY it points at then holds the real
  // image RVA. So resolve the data entry relative to the section, then resolve
  // its OffsetToData as an RVA.
  const dataEntry = rootOffset + langEntry.offToData
  const dataRva = u32(buf, dataEntry)
  const dataSize = u32(buf, dataEntry + 4)
  return { offset: rvaToOffset(sections, dataRva), size: dataSize }
}

// parseVersionInfo extracts the StringFileInfo key/value pairs from a
// VS_VERSION_INFO blob. FixedFileInfo is skipped; only the strings matter here.
function parseVersionInfo(blob) {
  const rootLen = u16(blob, 0)
  const rootValueLen = u16(blob, 2)
  const rootKey = readUtf16(blob, 6)
  if (rootKey !== 'VS_VERSION_INFO') throw new Error(`PE: unexpected version root key ${rootKey}`)

  const result = {}
  let cursor = align4(6 + (rootKey.length + 1) * 2) + rootValueLen
  const end = rootLen
  while (cursor < end) {
    const childLen = u16(blob, cursor)
    if (childLen === 0) break
    const childKey = readUtf16(blob, cursor + 6)
    if (childKey === 'StringFileInfo') {
      let childCursor = align4(cursor + 6 + (childKey.length + 1) * 2)
      const childEnd = cursor + childLen
      while (childCursor < childEnd) {
        const stLen = u16(blob, childCursor)
        if (stLen === 0) break
        // StringTable: its children are the actual String nodes.
        const stKey = readUtf16(blob, childCursor + 6)
        let stCursor = align4(childCursor + 6 + (stKey.length + 1) * 2)
        const stEnd = childCursor + stLen
        while (stCursor < stEnd) {
          const sLen = u16(blob, stCursor)
          if (sLen === 0) break
          const sValueLen = u16(blob, stCursor + 2)
          const sKey = readUtf16(blob, stCursor + 6)
          const valOffset = align4(stCursor + 6 + (sKey.length + 1) * 2)
          // wValueLength counts UTF-16 code units (winres writes value+"\0",
          // so it includes the terminator), not bytes — scale by 2 and strip
          // the trailing null to get the plain string.
          const value = blob.toString('utf16le', valOffset, valOffset + sValueLen * 2)
          result[sKey] = value.replace(/\0+$/, '')
          stCursor = align4(stCursor + sLen)
        }
        childCursor = align4(childCursor + stLen)
      }
    }
    cursor = align4(cursor + childLen)
  }
  return result
}

// versionStrings returns the VERSIONINFO string table keyed by field name
// (ProductName, FileDescription, CompanyName, …). Throws if the exe has no
// version resource.
export function versionStrings(buf) {
  const { sections } = parseHeaders(buf)
  const { offset, size } = findVersionData(buf, sections)
  return parseVersionInfo(buf.subarray(offset, offset + size))
}

// inspect returns the fields the self-check asserts on.
export function inspect(buf) {
  return { subsystem: subsystem(buf), versionStrings: versionStrings(buf) }
}

// inspectFile reads a PE from disk and inspects it.
export async function inspectFile(path) {
  const fs = await import('node:fs/promises')
  return inspect(await fs.readFile(path))
}
