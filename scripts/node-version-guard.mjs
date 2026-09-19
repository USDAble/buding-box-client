// Enforces the Node floor shared by local development and CI. Keeping this as a
// zero-dependency script makes an unsupported runtime fail before npm/Vite emit
// an opaque module error.

export const MIN_NODE = [22, 22, 2]

export function parseVersion(value) {
  const match = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(value.trim())
  return match ? match.slice(1).map(Number) : null
}

export function supported(version) {
  if (!version) return false
  for (let index = 0; index < MIN_NODE.length; index += 1) {
    if (version[index] > MIN_NODE[index]) return true
    if (version[index] < MIN_NODE[index]) return false
  }
  return true
}

export function problem(version = process.version) {
  const parsed = parseVersion(version)
  if (supported(parsed)) return null
  return `Node.js ${MIN_NODE.join('.')} or newer is required; found ${version}. Use nvm install/use (the repository pins ${MIN_NODE.join('.')} in .nvmrc).`
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const message = problem()
  if (message) {
    console.error(`node-version-guard failed: ${message}`)
    process.exitCode = 1
  } else {
    console.log(`node-version-guard passed: ${process.version} satisfies >=${MIN_NODE.join('.')}.`)
  }
}
