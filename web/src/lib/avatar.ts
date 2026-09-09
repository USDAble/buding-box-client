// Avatar rendering for the account corner and panel (P5): the nickname's
// first rune on a coloured disc. The brand's primary colour was not final at
// P5 time (需求 §10 T2), so the disc colour comes from a fixed neutral palette
// picked by hashing the nickname — stable across sessions without storing
// anything, and independent of the eventual brand colour (one mapping change
// when it lands).

// A muted, brand-neutral palette. Deliberately not the product blue: the disc
// must read as "this account", not "this product's accent".
const AVATAR_PALETTE = [
  "#5B8FF9", // blue
  "#5AD8A6", // green
  "#F6BD16", // amber
  "#E8684A", // coral
  "#6DC8EC", // sky
  "#9270CA", // violet
];

/**
 * The rune to render: the first code point of the trimmed nickname. Han keeps
 * its glyph, Latin is uppercased to match the corner's "initial" look, and an
 * empty nickname falls back to "?". Emoji (not a legal nickname character,
 * but a stray input must not crash the avatar) renders as itself.
 */
export function avatarInitial(nickname: string): string {
  const first = Array.from(nickname.trim())[0];
  if (!first) return "?";
  return first.toUpperCase();
}

/**
 * Palette index for a nickname, stable for a given name. Sum-of-codepoints is
 * deliberately simple: the palette is 6 colours and the requirement is only
 * that the same nickname always gets the same disc and neighbours don't
 * collide constantly.
 */
export function avatarColorIndex(nickname: string): number {
  let sum = 0;
  for (const ch of nickname) sum += ch.codePointAt(0)!;
  return sum % AVATAR_PALETTE.length;
}

export function avatarColor(nickname: string): string {
  return AVATAR_PALETTE[avatarColorIndex(nickname)];
}
