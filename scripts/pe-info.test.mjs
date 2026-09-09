// Tests for scripts/pe-info.mjs. Fixtures are hand-built minimal PE images
// rather than real binaries so the suite is hermetic (no Go toolchain, no
// checked-in .exe): buildPE lays out a PE32+ image, and buildResourceSection
// synthesizes a valid RT_VERSION resource tree the reader must walk.

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { subsystem, versionStrings, inspect } from './pe-info.mjs'

function align4(n) {
  return (n + 3) & ~3
}

// node builds one VS_VERSION_INFO structure: [wLength][wValueLength][wType]
// [key\0 (UTF-16, DWORD-padded)][value][children…], DWORD-aligned as a whole.
function node(key, wValueLength, wType, valueBuf, children = []) {
  const keyBuf = Buffer.from(key + '\0', 'utf16le')
  const valueStart = align4(6 + keyBuf.length)
  const pad = valueStart - (6 + keyBuf.length)
  const body = Buffer.concat([keyBuf, Buffer.alloc(pad), valueBuf, ...children])
  const total = align4(6 + body.length)
  const out = Buffer.alloc(total)
  out.writeUInt16LE(total, 0)
  out.writeUInt16LE(wValueLength, 2)
  out.writeUInt16LE(wType, 4)
  body.copy(out, 6)
  return out
}

function buildVersionInfo(strings) {
  const fixedInfo = Buffer.alloc(52)
  fixedInfo.writeUInt32LE(0xfeef04bd, 0) // VS_FIXEDFILEINFO signature
  // Mirrors winres' stringBytes: value is encoded with a trailing "\0" and
  // wValueLength counts UTF-16 code units (chars + 1), not bytes.
  const stringNodes = strings.map(([k, v]) => node(k, v.length + 1, 1, Buffer.from(v + '\0', 'utf16le')))
  const stringTable = node('040904B0', 0, 1, Buffer.alloc(0), stringNodes)
  const stringFileInfo = node('StringFileInfo', 0, 1, Buffer.alloc(0), [stringTable])
  return node('VS_VERSION_INFO', fixedInfo.length, 0, fixedInfo, [stringFileInfo])
}

// buildResourceSection lays out a resource tree for one version resource.
// It mirrors winres' layout: directory entries (subdir and leaf alike) are
// section-relative offsets, while the IMAGE_RESOURCE_DATA_ENTRY.OffsetToData
// is an image RVA (= virtualAddress + section offset) — the reader must
// resolve the two differently.
function buildResourceSection(versionBlob, virtualAddress = 0x1000) {
  const typeDirOff = 0x00
  const nameDirOff = 0x18
  const langDirOff = 0x30
  const dataEntryOff = 0x48
  const blobOff = 0x58

  const dir = (entries) => {
    const b = Buffer.alloc(16 + entries.length * 8)
    b.writeUInt16LE(0, 12) // NumberOfNamedEntries
    b.writeUInt16LE(entries.length, 14) // NumberOfIdEntries
    entries.forEach((en, i) => {
      b.writeUInt32LE(en.name >>> 0, 16 + i * 8)
      b.writeUInt32LE(en.offToData >>> 0, 16 + i * 8 + 4)
    })
    return b
  }

  const typeDir = dir([{ name: 16, offToData: nameDirOff | 0x80000000 }])
  const nameDir = dir([{ name: 1, offToData: langDirOff | 0x80000000 }])
  const langDir = dir([{ name: 0x0409, offToData: dataEntryOff }]) // section-relative leaf

  const dataEntry = Buffer.alloc(16)
  dataEntry.writeUInt32LE(virtualAddress + blobOff, 0) // OffsetToData (image RVA)
  dataEntry.writeUInt32LE(versionBlob.length, 4) // Size

  const data = Buffer.alloc(blobOff + versionBlob.length)
  typeDir.copy(data, typeDirOff)
  nameDir.copy(data, nameDirOff)
  langDir.copy(data, langDirOff)
  dataEntry.copy(data, dataEntryOff)
  versionBlob.copy(data, blobOff)
  return data
}

// buildPE assembles a minimal PE32+ image with an optional single .rsrc section.
// The .rsrc section gets a non-zero VirtualAddress so section-relative offsets
// and image RVAs differ, forcing the reader to resolve each correctly.
function buildPE({ subsystemValue = 2, resourceData = null, virtualAddress = 0x1000 } = {}) {
  const e = 0x80
  const sizeOfOptionalHeader = 240
  const numSections = resourceData ? 1 : 0

  const dos = Buffer.alloc(e)
  dos.writeUInt16LE(0x5a4d, 0)
  dos.writeUInt32LE(e, 0x3c)

  const coff = Buffer.alloc(20)
  coff.writeUInt16LE(0x8664, 0) // Machine: x64
  coff.writeUInt16LE(numSections, 2)
  coff.writeUInt16LE(sizeOfOptionalHeader, 16)

  const opt = Buffer.alloc(sizeOfOptionalHeader)
  opt.writeUInt16LE(0x20b, 0) // PE32+ magic
  opt.writeUInt16LE(subsystemValue, 68)
  opt.writeUInt32LE(16, 108) // NumberOfRvaAndSizes

  let sectionTable = Buffer.alloc(0)
  let sectionData = Buffer.alloc(0)
  if (resourceData) {
    sectionTable = Buffer.alloc(40)
    sectionTable.write('.rsrc\0\0\0', 0, 'ascii')
    sectionTable.writeUInt32LE(resourceData.length, 8) // VirtualSize
    sectionTable.writeUInt32LE(virtualAddress, 12) // VirtualAddress
    sectionTable.writeUInt32LE(resourceData.length, 16) // SizeOfRawData
    const sectionDataOff = 0x200
    sectionTable.writeUInt32LE(sectionDataOff, 20) // PointerToRawData
    const padding = sectionDataOff - (0x80 + 4 + 20 + sizeOfOptionalHeader + 40)
    sectionData = Buffer.concat([Buffer.alloc(padding), resourceData])
  }

  return Buffer.concat([dos, Buffer.from('PE\0\0', 'ascii'), coff, opt, sectionTable, sectionData])
}

test('subsystem: GUI (2) and console (3)', () => {
  assert.equal(subsystem(buildPE({ subsystemValue: 2 })), 2)
  assert.equal(subsystem(buildPE({ subsystemValue: 3 })), 3)
})

test('versionStrings: reads the string table keyed by field name', () => {
  const pe = buildPE({
    resourceData: buildResourceSection(
      buildVersionInfo([
        ['ProductName', 'Pudding Box'],
        ['FileDescription', '布丁盒子'],
        ['CompanyName', 'Pudding Box Studio'],
      ]),
    ),
  })
  assert.deepEqual(versionStrings(pe), {
    ProductName: 'Pudding Box',
    FileDescription: '布丁盒子',
    CompanyName: 'Pudding Box Studio',
  })
})

test('versionStrings: survives an odd-length key (DWORD padding)', () => {
  // "FileDescription" is 15 chars → key byte length 32, which lands the value
  // on a non-aligned boundary before padding. The reader must still find it.
  const pe = buildPE({
    resourceData: buildResourceSection(buildVersionInfo([['FileDescription', 'x']])),
  })
  assert.deepEqual(versionStrings(pe), { FileDescription: 'x' })
})

test('versionStrings: throws when there is no .rsrc section', () => {
  assert.throws(() => versionStrings(buildPE()), /no \.rsrc section/)
})

test('inspect: throws on a non-PE buffer', () => {
  assert.throws(() => inspect(Buffer.alloc(128, 0x41)), /missing MZ signature/)
})

test('inspect: combines subsystem and version strings', () => {
  const pe = buildPE({
    resourceData: buildResourceSection(buildVersionInfo([['ProductName', 'Pudding Box']])),
  })
  assert.deepEqual(inspect(pe), {
    subsystem: 2,
    versionStrings: { ProductName: 'Pudding Box' },
  })
})
