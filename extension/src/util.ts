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

export function human(n: number): string {
  if (!(n > 0)) return "0 B";
  if (n < 1024) return `${n} B`;
  const u = "KMGTP";
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < u.length - 1);
  return `${n.toFixed(n >= 100 ? 0 : 1)} ${u[i]}iB`;
}

// ---- video grabbing (content script) ----

/** One downloadable rendition of a video shown in the overlay menu. */
export interface VideoSource {
  url: string;
  label: string;
  /** "file": ODM can fetch it; "stream": HLS/DASH playlist, not supported yet. */
  kind: "file" | "stream";
  height?: number;
  playing?: boolean;
}

export interface VideoGroup {
  title: string;
  sources: VideoSource[];
}

/** Raw facts about a source before labelling (from <source>, a site playlist, ...). */
export interface RawSource {
  url: string;
  type?: string;
  label?: string;
  height?: number | string;
  width?: number | string;
}

/**
 * What kind of media URL this is; null for things ODM can never fetch
 * (blob: from Media Source Extensions, data:, mediastream:).
 */
export function mediaKind(url: string, type = ""): "file" | "stream" | null {
  if (!/^(https?|ftp):/i.test(url)) return null;
  const path = url.split(/[?#]/)[0].toLowerCase();
  if (/\.(m3u8|mpd)$/.test(path) || /mpegurl|dash\+xml/i.test(type)) return "stream";
  return "file";
}

const MEDIA_EXT = /\.(mp4|m4v|webm|mkv|mov|ogv|ogg|avi|flv|wmv|3gp|ts|mp3|m4a|aac|flac|wav|opus|m3u8|mpd)$/i;

/** True when the URL path ends in a known audio/video extension. */
export function isMediaUrl(url: string): boolean {
  return MEDIA_EXT.test(url.split(/[?#]/)[0]);
}

/** "MP4", "WEBM", ... from the URL's extension, else from a MIME type or a bare type like "mp4". */
export function formatOf(url: string, type = ""): string {
  const ext = url.split(/[?#]/)[0].match(MEDIA_EXT)?.[1];
  if (ext) return ext.toUpperCase();
  const t = type.split(";")[0].trim().toLowerCase();
  if (!t) return "";
  const sub = t.includes("/") ? t.split("/")[1] : t;
  if (/mpegurl/.test(sub)) return "HLS";
  if (/dash/.test(sub)) return "DASH";
  return sub.replace(/^x-/, "").toUpperCase();
}

const num = (v: unknown) => {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.round(n) : undefined;
};

/** Height in pixels from explicit data, else from a "720p"-style hint in the label or URL. */
export function heightOf(s: RawSource): number | undefined {
  const h = num(s.height);
  if (h) return h;
  const m = `${s.label ?? ""} ${s.url.split(/[?#]/)[0]}`.match(/(?:^|[^\d])(\d{3,4})p(?:[^a-z]|$)/i);
  return m ? num(m[1]) : undefined;
}

/** "480p · MP4" style label; falls back to the file name when nothing else is known. */
export function sourceLabel(s: RawSource): string {
  const h = heightOf(s);
  const quality = (s.label ?? "").trim() || (h ? `${h}p` : "");
  const fmt = formatOf(s.url, s.type);
  const parts = [quality, fmt && !quality.toUpperCase().includes(fmt) ? fmt : ""].filter(Boolean);
  if (parts.length) return parts.join(" · ");
  const last = s.url.split(/[?#]/)[0].split("/").filter(Boolean).pop() ?? s.url;
  try {
    return decodeURIComponent(last);
  } catch {
    return last;
  }
}

/** RawSource -> VideoSource, or null when ODM cannot fetch it at all. */
export function toSource(s: RawSource, base: string): VideoSource | null {
  let url: string;
  try {
    url = new URL(s.url, base).href;
  } catch {
    return null;
  }
  const kind = mediaKind(url, s.type);
  if (!kind) return null;
  return { url, label: sourceLabel({ ...s, url }), kind, height: heightOf({ ...s, url }) };
}

/**
 * archive.org's <play-av playlist='[...]'>: every video of the item with every
 * rendition (e.g. 480p MP4 and 300p OGV). Bad JSON yields no groups.
 */
export function archivePlaylist(json: string, base: string): VideoGroup[] {
  let items: unknown;
  try {
    items = JSON.parse(json);
  } catch {
    return [];
  }
  if (!Array.isArray(items)) return [];
  const groups: VideoGroup[] = [];
  for (const it of items as { title?: string; orig?: string; sources?: { file?: string; type?: string; label?: string; height?: string; width?: string }[] }[]) {
    const sources = (it.sources ?? [])
      .filter((s) => typeof s.file === "string" && s.file)
      .map((s) => toSource({ url: s.file!, type: s.type, label: s.label, height: s.height, width: s.width }, base))
      .filter((s): s is VideoSource => s !== null);
    if (sources.length) groups.push({ title: String(it.title || it.orig || ""), sources });
  }
  return groups;
}

/**
 * Drops URLs already listed in an earlier group (the first, most descriptive
 * label wins), marks the playing URL, sorts each group best quality first and
 * removes empty groups.
 */
export function mergeGroups(groups: VideoGroup[], playing = ""): VideoGroup[] {
  const seen = new Set<string>();
  const out: VideoGroup[] = [];
  for (const g of groups) {
    const sources: VideoSource[] = [];
    for (const s of g.sources) {
      if (seen.has(s.url)) continue;
      seen.add(s.url);
      sources.push(s.url === playing ? { ...s, playing: true } : s);
    }
    // Files before streams, then by height (unknown last); sort is stable.
    sources.sort((a, b) => (a.kind === b.kind ? 0 : a.kind === "file" ? -1 : 1) || (b.height ?? 0) - (a.height ?? 0));
    if (sources.length) out.push({ title: g.title, sources });
  }
  return out;
}
