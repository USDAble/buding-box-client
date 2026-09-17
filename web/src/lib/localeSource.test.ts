// 需求基线 E6.1 / L-E5: the frontend must not GUESS the language — the app
// language comes from the desktop shell once, is changeable, and is persisted.
// `navigator.language` is the browser's preference, not the user's choice, so
// reading it puts the UI in a language the user never picked.
//
// This is a source scan rather than a behavioural test because the failure is a
// second source appearing anywhere in the tree, including in a path no test
// happens to render. It caught one: ChatView's export locale chain
// (`document.documentElement.lang || navigator.language || 'en'`) always bottomed
// out at the browser, because nothing sets documentElement.lang — so a user who
// picked 中文 on an English machine got English PNG/HTML exports.
//
// Comments are stripped before scanning: prose may discuss the browser's
// preference (that is how the reasoning stays in the tree) without being a read.
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { SRC, sourceFiles, stripComments } from '../test/sourceScan'

const FORBIDDEN = 'navigator.language'

describe('the app language has exactly one source', () => {
  it('nothing under web/src reads the browser language', () => {
    const offenders = sourceFiles(SRC)
      .filter((path) => stripComments(readFileSync(path, 'utf8')).includes(FORBIDDEN))
      .map((path) => path.slice(SRC.length + 1))

    expect(
      offenders,
      `${FORBIDDEN} is the browser's preference, not the user's choice ` +
        '(需求基线 E6.1 / L-E5). Read the locale store instead — exportTranscript.ts `' +
        'is the reference:',
    ).toEqual([])
  })
})
