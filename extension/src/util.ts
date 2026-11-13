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
  /** "file": one file; "hls": HLS playlist ODM joins into one file; "stream": DASH, not supported yet. */
  kind: SourceKind;
  height?: number;
  playing?: boolean;
  /** HLS: estimated size in bytes (bandwidth x duration). */
  estimate?: number;
}

export type SourceKind = "file" | "hls" | "stream";

export interface VideoGroup {
  title: string;
  sources: VideoSource[];
  /** Title is a UI label ("This video"), not the video's name: use the page title for files. */
  generic?: boolean;
}

/**
 * A file name for a picked rendition: "<title> (<quality>).<ext>", safe on
 * Windows. Falls back to the URL's own name when there is no title.
 */
export function fileNameFor(title: string, s: Pick<VideoSource, "url" | "label" | "height" | "kind">): string {
  const clean = title
    .replace(/[<>:"/\\|?*\u0000-\u001f]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/[. ]+$/, "")
    .slice(0, 150)
    .trim();
  // HLS/DASH end up as an MP4 after ODM joins the pieces.
  const ext = s.kind !== "file" ? "mp4" : (formatOf(s.url) || "mp4").toLowerCase();
  if (!clean) {
    const last = s.url.split(/[?#]/)[0].split("/").filter(Boolean).pop() ?? "video";
    try {
      return decodeURIComponent(last);
    } catch {
      return last;
    }
  }
  const quality = s.height ? `${s.height}p` : "";
  return `${clean}${quality ? ` (${quality})` : ""}.${ext}`;
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
export function mediaKind(url: string, type = ""): SourceKind | null {
  if (!/^(https?|ftp):/i.test(url)) return null;
  const path = url.split(/[?#]/)[0].toLowerCase();
  if (/\.m3u8$/.test(path) || /mpegurl/i.test(type)) return "hls";
  if (/\.mpd$/.test(path) || /dash\+xml/i.test(type)) return "stream";
  return "file";
}

// ---- HLS playlists (content.ts gets them from hook.ts) ----

export const isHlsMaster = (text: string) => text.includes("#EXT-X-STREAM-INF");

export interface HlsVariant {
  url: string;
  height?: number;
  bandwidth?: number; // bits/s, AVERAGE-BANDWIDTH when given
}

/** Variants of a master playlist, best first. Relative URIs resolve against base. */
export function hlsVariants(text: string, base: string): HlsVariant[] {
  const out: HlsVariant[] = [];
  let pending: HlsVariant | null = null;
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    if (line.startsWith("#EXT-X-STREAM-INF:")) {
      const attr = (k: string) => line.match(new RegExp(`[:,]${k}=("[^"]*"|[^,]*)`))?.[1]?.replace(/"/g, "");
      const res = attr("RESOLUTION")?.split("x");
      pending = { url: "", height: num(res?.[1]), bandwidth: num(attr("AVERAGE-BANDWIDTH")) ?? num(attr("BANDWIDTH")) };
    } else if (!line.startsWith("#") && pending) {
      try {
        pending.url = new URL(line, base).href;
        out.push(pending);
      } catch {
        /* skip a bad URI */
      }
      pending = null;
    }
  }
  return out.sort((a, b) => (b.height ?? 0) - (a.height ?? 0) || (b.bandwidth ?? 0) - (a.bandwidth ?? 0));
}

/** Total duration of a media playlist in seconds (sum of #EXTINF). */
export function hlsDuration(text: string): number {
  let d = 0;
  for (const m of text.matchAll(/#EXTINF:\s*([\d.]+)/g)) d += Number(m[1]) || 0;
  return d;
}

const MEDIA_EXT = /\.(mp4|m4v|webm|mkv|mov|ogv|ogg|avi|flv|wmv|3gp|ts|mp3|m4a|aac|flac|wav|opus|m3u8|mpd)$/i;

/** True when the URL path ends in a known audio/video extension. */
export function isMediaUrl(url: string): boolean {
  return MEDIA_EXT.test(url.split(/[?#]/)[0]);
}

/** "MP4", "WEBM", ... from the URL's extension, else from a MIME type or a bare type like "mp4". */
export function formatOf(url: string, type = ""): string {
  const ext = url.split(/[?#]/)[0].match(MEDIA_EXT)?.[1];
  if (ext) return ({ m3u8: "HLS", mpd: "DASH" } as Record<string, string>)[ext.toLowerCase()] ?? ext.toUpperCase();
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
  const hint = `${s.label ?? ""} ${s.url.split(/[?#]/)[0]}`;
  const p = hint.match(/(?:^|[^\d])(\d{3,4})p(?:[^a-z]|$)/i);
  if (p) return num(p[1]);
  // "..._1920x1080.mp4" (Ciner CDN and many others)
  const wh = hint.match(/(?:^|[^\d])\d{3,4}x(\d{3,4})(?:[^\d]|$)/i);
  return wh ? num(wh[1]) : undefined;
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
 * Cheap pre-check before JSON.parse: player configs worth parsing are JSON
 * objects/arrays that mention a media file somewhere.
 */
export function mayHoldMedia(text: string | null | undefined): text is string {
  if (!text || text.length < 20) return false;
  const c = text.trimStart()[0];
  return (c === "{" || c === "[") && /\.(mp4|m4v|webm|mkv|mov|ogv|m3u8|mpd|mp3|m4a)\b/i.test(text);
}

const str = (v: unknown) => (typeof v === "string" && v.trim() ? v.trim() : undefined);
const numOrStr = (v: unknown) => (typeof v === "number" || typeof v === "string" ? v : undefined);

/**
 * Media sources in a player config found in the page, whatever the player:
 * archive.org <play-av playlist>, Ciner's data-hope-video (Show TV, Habertürk),
 * video.js data-setup, JSON-LD VideoObject, ... An object with src/file/url
 * pointing at media is one source (label/height/type beside it); sources are
 * grouped by the nearest "title"/"name" above them. Bad JSON yields nothing.
 */
export function jsonSources(text: string, base: string): VideoGroup[] {
  let root: unknown;
  try {
    root = JSON.parse(text);
  } catch {
    return [];
  }
  const groups = new Map<string, VideoSource[]>();
  const add = (title: string, raw: RawSource) => {
    const s = toSource(raw, base);
    if (!s) return;
    if (!groups.has(title)) groups.set(title, []);
    groups.get(title)!.push(s);
  };
  const walk = (node: unknown, title: string, depth: number) => {
    if (depth > 12 || node === null || typeof node !== "object") return;
    if (Array.isArray(node)) {
      for (const x of node) walk(x, title, depth + 1);
      return;
    }
    const o = node as Record<string, unknown>;
    const t = str(o.title) ?? str(o.name) ?? title;
    const url = str(o.src) ?? str(o.file) ?? str(o.url) ?? str(o.contentUrl);
    const type = str(o.type) ?? str(o.encodingFormat) ?? "";
    if (url && (isMediaUrl(url) || /^(video|audio)\/|mpegurl|dash/i.test(type))) {
      add(t, { url, type, label: str(o.label) ?? str(o.quality), height: numOrStr(o.height ?? o.res), width: numOrStr(o.width) });
      return;
    }
    for (const v of Object.values(o)) {
      if (typeof v === "string") {
        // e.g. {"hls": "https://.../master.m3u8"}. Absolute only: a bare name like
        // archive.org's "orig": "Cops-v2.mp4" would resolve against the page URL.
        if (/^https?:\/\//i.test(v) && isMediaUrl(v)) add(t, { url: v });
      } else walk(v, t, depth + 1);
    }
  };
  walk(root, "", 0);
  return [...groups].map(([title, sources]) => ({ title, sources }));
}

/** A file's own name when it is specific enough to identify it across mirrors. */
function distinctName(s: VideoSource): string {
  if (s.kind !== "file") return "";
  const last = s.url.split(/[?#]/)[0].split("/").pop() ?? "";
  const stem = last.replace(/\.[^.]+$/, "").toLowerCase();
  if (stem.length < 6 || /^(index|video|master|playlist|media|stream|file|default|source|movie)\d*$/.test(stem)) return "";
  return last.toLowerCase();
}

/**
 * Drops URLs already listed in an earlier group (the first, most descriptive
 * label wins), marks the playing URL, sorts each group best quality first and
 * removes empty groups.
 */
export function mergeGroups(groups: VideoGroup[], playing = ""): VideoGroup[] {
  const seen = new Set<string>();
  const out: VideoGroup[] = [];
  // Same title from two configs (JSON-LD + player attribute) is one video.
  const byTitle = new Map<string, VideoGroup>();
  for (const g of groups) {
    const sources: VideoSource[] = [];
    for (const s of g.sources) {
      if (seen.has(s.url)) continue;
      seen.add(s.url);
      sources.push(s.url === playing ? { ...s, playing: true } : s);
    }
    if (!sources.length) continue;
    const same = g.title && !g.generic ? byTitle.get(g.title) : undefined;
    if (same) same.sources.push(...sources);
    else {
      const ng: VideoGroup = { title: g.title, sources, ...(g.generic ? { generic: true } : {}) };
      out.push(ng);
      if (g.title && !g.generic) byTitle.set(g.title, ng);
    }
  }
  // The player often streams a listed file from a redirected mirror
  // (archive.org/download/x.mp4 -> ia8xxxx.us.archive.org/.../x.mp4): same
  // file name, different URL. Keep the first, more descriptive entry.
  const byName = new Map<string, VideoSource>();
  for (const g of out) {
    g.sources = g.sources.filter((s) => {
      const name = distinctName(s);
      const first = name ? byName.get(name) : undefined;
      if (!first) {
        if (name) byName.set(name, s);
        return true;
      }
      if (s.playing) first.playing = true;
      return false;
    });
  }
  out.splice(0, out.length, ...out.filter((g) => g.sources.length));

  // Files, then HLS, then unsupported streams; by height (unknown last); sort is stable.
  const rank = { file: 0, hls: 1, stream: 2 };
  for (const g of out) g.sources.sort((a, b) => rank[a.kind] - rank[b.kind] || (b.height ?? 0) - (a.height ?? 0));
  return out;
}
