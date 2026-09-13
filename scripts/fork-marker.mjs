// scripts/fork-marker.mjs
//
// The definition of an `OCTO-FORK:` marker line. One owner, two consumers
// (开发规范 §3.8): the definition lives here, and the two guards that need the
// same answer to "is this line a marker?" import it.
//
//   fork-marker-guard.mjs  asserts that every modified upstream file carries
//                          one (hard rule 3, V-48).
//   server-diff-guard.mjs  must NOT count one as fork debt. A marker line is
//                          mandated by hard rule 3, so charging it against the
//                          debt ratchet makes the two guards contradict:
//                          complying with one would fail the other. Measured
//                          2026-09-13, the markers inside governed files were
//                          server.go 3, handlers.go 1, attachments.go 2,
//                          lightapps_handlers.go 1 — small, but the *rule* is
//                          what matters, not the size.
//
// WHY A SEPARATE MODULE RATHER THAN AN IMPORT FROM THE GUARD. The marker guard
// already imports git/resolveUpstream from server-diff-guard.mjs, which is the
// shared base; importing the pattern back out of the guard would be a cycle.
// A leaf module that imports nothing keeps the direction one-way.

// A marker LINE: whitespace, a comment introducer, the token, and a colon.
// The colon is load-bearing. Without it prose *about* the rule matches — which
// is exactly how the loose `grep -c "OCTO-FORK"` census over-counted by 24
// files: CLAUDE.md and .octorules contain the token because they define the
// rule, and a tool that merely mentions it would pass. The introducers cover:
//   //      Go, TypeScript, Svelte <script>, C
//   #       YAML, shell, .desktop, Makefile
//   <!--    Markdown, HTML, XML, plist
//   /* *    block-comment continuations
//   ;       Inno Setup (.iss)
//   --      Lua, SQL
//   %       TeX, Erlang
//   '       Visual Basic
//
// The introducer group repeats: `;;` is a legal Inno Setup comment, and `**`
// opens a JSDoc block continuation.
export const MARKER_PATTERN =
  /^[ \t]*(?:(?:\/\/|#|<!--|\/\*|\*|;|--|%|')[ \t]*)+OCTO-FORK:/m

// isMarkerLine answers for a single line. The pattern is not global, so `test`
// carries no lastIndex state between calls.
export function isMarkerLine(line) {
  return MARKER_PATTERN.test(line)
}
