// How the generated-file guards compare a committed artifact with what the
// generator would emit.
//
// Content, not bytes (V-104, 需求基线 §5.6). A Windows checkout rewrites every
// text file to CRLF — `actions/checkout` sets no `core.autocrlf`, so the runner's
// Git for Windows default applies — while a generator joins its *own* lines with
// `\n` and only inherits CRLF from the file it read. The two sides therefore
// differ by line endings alone, and the comparison never converges: regenerating
// on Windows writes another mixed file.
//
// The failure this fixes was permanent, not intermittent: `agents-guard` was red
// in every Windows packaging leg while the same commit passed the ubuntu Agents
// Guard job, so the product's primary deliverable platform could never reach a
// green preflight. Reproduced on macOS by converting a clean checkout to CRLF —
// byte-for-byte the same message.
export function normalizeEol(text) {
  return text.replaceAll('\r\n', '\n')
}

// sameGeneratedText reports whether two generated-file contents agree, ignoring
// the line-ending form the working tree happens to use.
export function sameGeneratedText(a, b) {
  return normalizeEol(a) === normalizeEol(b)
}
