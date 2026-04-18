/**
 * Minimal ANSI escape sequence → React-friendly segments converter.
 *
 * Why not `ansi-to-html`? Two reasons:
 *   1. This keeps the bundle small — we only need foreground colors and
 *      bold/dim, which is ~80% of what log-emitting CLIs use in practice.
 *   2. Returning JSX-ready segments (rather than an HTML string) lets us
 *      avoid `dangerouslySetInnerHTML`. Safer + works with React's
 *      reconciler.
 *
 * Supported sequences:
 *   - CSI ... m (SGR): reset, bold/dim, foreground colors 30-37 / 90-97
 *   - 8-bit colors (CSI 38;5;N m) — mapped to the basic palette
 *   - 24-bit colors (CSI 38;2;r;g;b m) — passed through as style
 *
 * Unsupported sequences (background, italic, underline, cursor moves)
 * are silently stripped — their markup equivalents aren't worth the
 * bundle size for a log viewer.
 */

export interface AnsiSegment {
  text: string;
  className?: string;
  // Inline style used when the sequence specified a true-color RGB.
  style?: { color: string };
}

// Basic 16-color palette mapped to Tailwind color classes. Muted
// enough to stay readable on our dark log surface; bright-enough
// variants are used when bold is set.
const BASIC_FG_CLASS: Record<number, { normal: string; bright: string }> = {
  30: { normal: 'text-gray-500', bright: 'text-gray-400' }, // black
  31: { normal: 'text-red-400', bright: 'text-red-300' },
  32: { normal: 'text-green-400', bright: 'text-green-300' },
  33: { normal: 'text-yellow-400', bright: 'text-yellow-300' },
  34: { normal: 'text-blue-400', bright: 'text-blue-300' },
  35: { normal: 'text-purple-400', bright: 'text-purple-300' },
  36: { normal: 'text-cyan-400', bright: 'text-cyan-300' },
  37: { normal: 'text-gray-200', bright: 'text-white' },
};

 
const CSI_RE = /\x1b\[([0-9;]*)m/g;

/** Parse an ANSI-coded string into plain segments. */
export function parseAnsi(input: string): AnsiSegment[] {
  const segments: AnsiSegment[] = [];
  let cursor = 0;
  let currentClass: string | undefined;
  let currentStyle: { color: string } | undefined;
  let bold = false;

  CSI_RE.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = CSI_RE.exec(input)) !== null) {
    // Plain text up to this escape.
    if (match.index > cursor) {
      segments.push({
        text: input.slice(cursor, match.index),
        className: currentClass,
        style: currentStyle,
      });
    }
    cursor = match.index + match[0].length;

    // Apply SGR params. Empty string means ESC[m which resets.
    const paramStr = match[1];
    const params = paramStr === '' ? [0] : paramStr.split(';').map((s) => parseInt(s, 10));

    for (let i = 0; i < params.length; i++) {
      const p = params[i];
      if (p === 0) {
        currentClass = undefined;
        currentStyle = undefined;
        bold = false;
      } else if (p === 1) {
        bold = true;
        // Re-derive class with bright palette if fg was set.
      } else if (p === 22) {
        bold = false;
      } else if (p >= 30 && p <= 37) {
        const entry = BASIC_FG_CLASS[p];
        if (entry) currentClass = bold ? entry.bright : entry.normal;
        currentStyle = undefined;
      } else if (p >= 90 && p <= 97) {
        const entry = BASIC_FG_CLASS[p - 60];
        if (entry) currentClass = entry.bright;
        currentStyle = undefined;
      } else if (p === 38) {
        // Extended foreground: 38;5;N (256-color) or 38;2;r;g;b (true)
        const mode = params[i + 1];
        if (mode === 5 && i + 2 < params.length) {
          // Map 256-color into the basic 30-37 palette (crude but enough).
          const n = params[i + 2];
          const basic = 30 + (n % 8);
          const entry = BASIC_FG_CLASS[basic];
          if (entry) currentClass = bold ? entry.bright : entry.normal;
          currentStyle = undefined;
          i += 2;
        } else if (mode === 2 && i + 4 < params.length) {
          const r = params[i + 2];
          const g = params[i + 3];
          const b = params[i + 4];
          currentStyle = { color: `rgb(${r},${g},${b})` };
          currentClass = undefined;
          i += 4;
        }
      } else if (p === 39) {
        currentClass = undefined;
        currentStyle = undefined;
      }
      // Backgrounds (40-47, 100-107), italic (3), underline (4), etc. — ignored.
    }
  }

  // Trailing plain text.
  if (cursor < input.length) {
    segments.push({
      text: input.slice(cursor),
      className: currentClass,
      style: currentStyle,
    });
  }
  return segments;
}
