import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const source = path.join(root, 'branding', 'brand.json')
const brand = JSON.parse(await fs.readFile(source, 'utf8'))
const asset = path.join(root, 'branding', brand.visual.logo.mark)

const targets = [
  path.join(root, 'internal', 'brand', 'brand.json'),
  path.join(root, 'web', 'src', 'lib', 'brand.config.json'),
  path.join(root, 'mobile', 'src', 'brand.config.json'),
  path.join(root, 'web', 'public', 'favicon.svg'),
  path.join(root, 'landing', 'pudding-box-mark.svg'),
  path.join(root, 'landing', 'brand.config.json'),
  path.join(root, 'cmd', 'octo-relay', 'internal', 'push', 'brand.json'),
]

await fs.mkdir(path.join(root, 'internal', 'brand'), { recursive: true })
await fs.mkdir(path.join(root, 'web', 'src', 'lib'), { recursive: true })
await fs.mkdir(path.join(root, 'web', 'public'), { recursive: true })
await fs.mkdir(path.join(root, 'mobile', 'src'), { recursive: true })
await fs.mkdir(path.join(root, 'landing'), { recursive: true })
await fs.mkdir(path.join(root, 'cmd', 'octo-relay', 'internal', 'push'), { recursive: true })
await fs.copyFile(source, targets[0])
await fs.copyFile(source, targets[1])
await fs.copyFile(source, targets[2])
await fs.copyFile(asset, targets[3])
await fs.copyFile(asset, targets[4])
await fs.copyFile(source, targets[5])
await fs.copyFile(source, targets[6])
console.log(`Synced brand configuration and logo to ${targets.length} generated targets.`)
