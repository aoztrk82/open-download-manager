// Pure helpers (no browser APIs), so they can be tested with plain node.

/** File name from a Content-Disposition header (RFC 6266 filename* first). */
export function dispositionName(cd: string): string {
  const star = cd.match(/filename\*\s*=\s*(?:[\w-]+)?'[^']*'([^;]+)/i);
  if (star) {
    try {
      return decodeURIComponent(star[1].trim().replace(/^"|"$/g, ""));
    } catch {
      /* fall through */
    }
  }
  const plain = cd.match(/filename\s*=\s*("([^"]*)"|[^;]+)/i);
  return plain ? (plain[2] ?? plain[1]).trim() : "";
}
