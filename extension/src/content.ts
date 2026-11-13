// Video button. Hovering a video shows "Download with ODM" in its top-right
// corner; the menu lists every rendition this page reveals (player configs in
// JSON attributes/scripts, <source> tags, the playing URL, other videos,
// og:video) and hands the picked
// one to the background script, which opens the usual download dialog.
// Runs in every http(s) frame, so it must stay cheap until a video is hovered.
import { ext } from "./ext";
import { setLang, t } from "./i18n";
import {
  cleanTitle,
  fileNameFor,
  isYtdlpSite,
  ytFormat,
  hlsDuration,
  hlsVariants,
  human,
  isHlsMaster,
  isMediaUrl,
  jsonSources,
  mayHoldMedia,
  mergeGroups,
  toSource,
  type RawSource,
  type VideoGroup,
  type VideoSource,
} from "./util";

const MIN_W = 200; // smaller videos are previews/ads; no button
const MIN_H = 110;
const IDLE_HIDE_MS = 3000; // like player controls: go away when the mouse rests
const LEAVE_HIDE_MS = 700;

let enabled = true;
ext.storage.local.get({ videoButton: true, language: "auto" }).then(
  (s) => {
    enabled = s.videoButton !== false;
    setLang(s.language as string, false); // false: the document is the page's
  },
  () => {},
);
ext.storage.onChanged.addListener((c, area) => {
  if (area !== "local") return;
  if (c.videoButton) {
    enabled = c.videoButton.newValue !== false;
    if (!enabled) hide();
  }
  if (c.language) {
    setLang(c.language.newValue as string, false);
    if (ui) ui.label.textContent = t("bg.menu");
  }
});

// ---- finding the video under the pointer (also inside shadow DOM) ----

type DomApi = { openOrClosedShadowRoot?(el: Element): ShadowRoot | null };

/** Content scripts may look into closed shadow roots too (custom players use them). */
function shadowOf(el: Element): ShadowRoot | null {
  try {
    const dom = (ext as unknown as { dom?: DomApi }).dom; // Chrome; newer Firefox
    if (dom?.openOrClosedShadowRoot) return dom.openOrClosedShadowRoot(el) ?? null;
    const ff = (el as unknown as { openOrClosedShadowRoot?: () => ShadowRoot | null }).openOrClosedShadowRoot;
    if (ff) return ff.call(el) ?? null;
  } catch {
    /* not an element that can host a shadow root */
  }
  return el.shadowRoot;
}

/**
 * The video under the point. Ad SDKs (Google IMA) stack their own <video> on
 * top of the real one, so a video that looks like an ad is only the fallback.
 */
function videoAt(x: number, y: number): HTMLVideoElement | null {
  const all = videosAt(x, y, document, new Set());
  return all.find((v) => !looksLikeAd(v)) ?? all[0] ?? null;
}

function videosAt(x: number, y: number, root: Document | ShadowRoot, seen: Set<Node>, out: HTMLVideoElement[] = []): HTMLVideoElement[] {
  seen.add(root);
  // All stacked elements, not just the top one: players cover the <video> with controls.
  for (const el of root.elementsFromPoint(x, y)) {
    if (el === host) continue;
    if (el.localName === "video") {
      if (!out.includes(el as HTMLVideoElement)) out.push(el as HTMLVideoElement);
      continue;
    }
    const sr = shadowOf(el);
    if (sr && !seen.has(sr)) videosAt(x, y, sr, seen, out);
  }
  return out;
}

const AD_SRC = /(^|\.)(2mdn\.net|doubleclick\.net|googlesyndication\.com|imasdk\.googleapis\.com|googleadservices\.com)$/i;
// Ad *containers* (Google IMA's "<player>_ima-ad-container", "ad-container"),
// not player state classes: video.js marks the real player "vjs-ad-playing".
const AD_BOX = /(ima-ad|ad-?container|ad-?video|adsbox|preroll|imasdk)/i;

function looksLikeAd(v: HTMLVideoElement): boolean {
  try {
    if (AD_SRC.test(new URL(v.currentSrc || v.src).hostname)) return true;
  } catch {
    /* blob:, empty */
  }
  for (let e: Element | null = v, i = 0; e && i < 6; e = e.parentElement, i++) {
    if (AD_BOX.test(e.id) || AD_BOX.test(typeof e.className === "string" ? e.className : "")) return true;
  }
  return false;
}

const bigEnough = (r: DOMRect) => r.width >= MIN_W && r.height >= MIN_H;

// ---- overlay (closed shadow root, so page CSS cannot reach it) ----

const CSS = `
:host { all: initial; }
.wrap { font: 13px/1.3 system-ui, -apple-system, "Segoe UI", sans-serif; color: #e5e7eb;
  display: flex; flex-direction: column; align-items: flex-end; gap: 6px; }
.btn { all: unset; display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: 6px;
  background: rgba(17,24,39,.88); border: 1px solid rgba(255,255,255,.2); color: #fff; cursor: pointer;
  box-shadow: 0 2px 8px rgba(0,0,0,.35); font-weight: 600; font-size: 12.5px; user-select: none; }
.btn:hover, .btn:focus-visible { background: #2563eb; }
.btn.ok { background: #15803d; } .btn.bad { background: #b91c1c; }
svg { width: 14px; height: 14px; fill: none; stroke: currentColor; stroke-width: 2.2;
  stroke-linecap: round; stroke-linejoin: round; flex: none; }
.menu { box-sizing: border-box; min-width: 240px; max-width: 380px; overflow: auto; padding: 4px;
  background: rgba(17,24,39,.97); border: 1px solid rgba(255,255,255,.15); border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,.45); }
.head { padding: 7px 8px 3px; font-size: 11px; text-transform: uppercase; letter-spacing: .04em; color: #9ca3af;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.item { all: unset; box-sizing: border-box; display: flex; align-items: center; gap: 8px; width: 100%;
  padding: 7px 8px; border-radius: 5px; cursor: pointer; color: #f3f4f6; }
.item:hover, .item:focus-visible { background: #2563eb; }
.item[disabled] { cursor: default; opacity: .55; background: none; }
.q { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tag { font-size: 10.5px; padding: 1px 6px; border-radius: 9px; background: rgba(34,197,94,.22); color: #86efac; }
.size { font-size: 11.5px; color: #9ca3af; white-space: nowrap; }
.item:hover .size { color: #dbeafe; }
.note { padding: 8px; color: #d1d5db; max-width: 300px; }
[hidden] { display: none !important; }
`;

function el(tag: string, cls = "", text = ""): HTMLElement {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text) e.textContent = text;
  return e;
}

function icon(d: string): SVGSVGElement {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  const p = document.createElementNS(ns, "path");
  p.setAttribute("d", d);
  svg.append(p);
  return svg;
}

interface Ui {
  wrap: HTMLElement;
  button: HTMLButtonElement;
  label: HTMLElement;
  menu: HTMLElement;
}

let host: HTMLElement | null = null;
let ui: Ui | null = null;
let target: HTMLVideoElement | null = null;
let menuOpen = false;
let hideTimer = 0;
let tracker = 0;
let flashTimer = 0;

function ensureUi(): Ui {
  if (ui) return ui;
  host = document.createElement("odm-video-grab");
  host.style.cssText = "all:initial;position:fixed;z-index:2147483647;top:0;right:0;display:none;";
  const root = host.attachShadow({ mode: "closed" });
  const style = el("style");
  style.textContent = CSS;
  const wrap = el("div", "wrap");
  const button = el("button", "btn") as HTMLButtonElement;
  button.type = "button";
  const label = el("span", "", t("bg.menu"));
  button.append(icon("M12 4v11M7 10l5 5 5-5M5 20h14"), label, icon("M6 9l6 6 6-6"));
  const menu = el("div", "menu");
  menu.hidden = true;
  wrap.append(button, menu);
  root.append(style, wrap);
  // The page's player must not see clicks meant for us (it would pause/play).
  for (const ev of ["pointerdown", "pointerup", "mousedown", "mouseup", "click", "dblclick", "contextmenu", "wheel"]) {
    wrap.addEventListener(ev, (e) => e.stopPropagation());
  }
  button.addEventListener("click", () => (menuOpen ? closeMenu() : openMenu()));
  ui = { wrap, button, label, menu };
  return ui;
}

/** Fullscreen shows only the fullscreen element's subtree, so the overlay moves into it. */
function mount() {
  if (!host) return;
  const fs = document.fullscreenElement;
  const parent = fs && fs.localName !== "video" ? fs : document.documentElement;
  if (host.parentNode !== parent) parent.append(host);
}
document.addEventListener("fullscreenchange", () => {
  if (!host) return;
  if (document.fullscreenElement?.localName === "video") hide(); // nothing can be drawn over it
  else mount();
  place();
});

function place() {
  if (!host || !target || !ui) return;
  if (!target.isConnected) return hide();
  const r = target.getBoundingClientRect();
  if (!bigEnough(r) || r.bottom < 0 || r.top > innerHeight) return hide();
  const top = Math.max(r.top, 0) + 8;
  const right = Math.max(document.documentElement.clientWidth - r.right, 0) + 8;
  host.style.top = `${top}px`;
  host.style.right = `${right}px`;
  // Keep the menu inside the video (and the viewport); it scrolls when longer.
  ui.menu.style.maxHeight = `${Math.max(140, Math.min(r.bottom, innerHeight) - top - 48)}px`;
}

function show(v: HTMLVideoElement) {
  if (target !== v) {
    closeMenu();
    target = v;
  }
  ensureUi();
  mount();
  host!.style.display = "block";
  place();
  if (!tracker) {
    // Videos move (layout, theatre mode, scrolling containers); follow them.
    tracker = window.setInterval(place, 300);
    addEventListener("scroll", place, { capture: true, passive: true });
    addEventListener("resize", place, { passive: true });
  }
}

function hide() {
  clearTimeout(hideTimer);
  closeMenu();
  target = null;
  if (host) host.style.display = "none";
  if (tracker) {
    clearInterval(tracker);
    tracker = 0;
    removeEventListener("scroll", place, { capture: true });
    removeEventListener("resize", place);
  }
}

function hideLater(ms: number) {
  clearTimeout(hideTimer);
  hideTimer = window.setTimeout(() => !menuOpen && hide(), ms);
}

/** The pointer is at (x, y) in this frame's viewport. */
function pointerAt(x: number, y: number) {
  const v = videoAt(x, y);
  if (v && bigEnough(v.getBoundingClientRect())) {
    show(v);
    hideLater(IDLE_HIDE_MS);
    return;
  }
  if (target && !menuOpen) hideLater(LEAVE_HIDE_MS);
  // No video here: an iframe laid over a player (ad SDKs keep one there)
  // swallows every mouse event, and Chrome tells the page nothing about a
  // pointer inside a cross-process iframe. So pass the position up; the
  // parent checks what is under it there.
  if (window !== window.top) {
    try {
      window.parent.postMessage({ __odm_ptr__: 1, x, y }, "*");
    } catch {
      /* detached */
    }
  }
}

let lastMove = 0;
document.addEventListener(
  "mousemove",
  (e) => {
    if (!enabled) return;
    const now = performance.now();
    if (now - lastMove < 120) return; // elementsFromPoint is not free; 8 checks a second is plenty
    lastMove = now;
    pointerAt(e.clientX, e.clientY);
  },
  { capture: true, passive: true },
);
// Pointer positions from child frames (see pointerAt), in their coordinates.
window.addEventListener("message", (e) => {
  const d = e.data as { __odm_ptr__?: number; x?: unknown; y?: unknown } | null;
  if (!enabled || !d || d.__odm_ptr__ !== 1 || typeof d.x !== "number" || typeof d.y !== "number" || !e.source) return;
  let frame: HTMLIFrameElement | undefined;
  document.querySelectorAll("iframe").forEach((f) => {
    if (f.contentWindow === e.source) frame = f;
  });
  if (!frame) return;
  const r = frame.getBoundingClientRect();
  pointerAt(r.left + frame.clientLeft + d.x, r.top + frame.clientTop + d.y);
});
// A video that starts playing gets the button for a moment without any mouse
// move (media events do not bubble, but capture listeners see them).
document.addEventListener(
  "playing",
  (e) => {
    const v = e.target as HTMLVideoElement;
    if (!enabled || v.localName !== "video" || looksLikeAd(v) || menuOpen) return;
    if (target && target !== v) return; // the user is already on another video
    if (bigEnough(v.getBoundingClientRect())) {
      show(v);
      hideLater(IDLE_HIDE_MS);
    }
  },
  true,
);
document.documentElement.addEventListener("mouseleave", () => target && !menuOpen && hideLater(LEAVE_HIDE_MS));
document.addEventListener(
  "pointerdown",
  (e) => {
    if (menuOpen && host && !e.composedPath().includes(host)) closeMenu();
  },
  true,
);
document.addEventListener(
  "keydown",
  (e) => {
    if (menuOpen && e.key === "Escape") closeMenu();
  },
  true,
);

// ---- HLS playlists seen by hook.ts (page world) ----

// URL -> playlist text, newest last. Page scripts can post these messages too,
// so they are only ever offered in the menu, never acted on by themselves.
const captured = new Map<string, string>();
window.addEventListener("message", (e) => {
  if (e.source !== window) return;
  const d = e.data as { __odm_hls__?: number; url?: unknown; text?: unknown } | null;
  if (!d || d.__odm_hls__ !== 1 || typeof d.url !== "string" || typeof d.text !== "string") return;
  if (!/^https?:/i.test(d.url) || d.text.length > 16 << 20) return;
  captured.delete(d.url);
  captured.set(d.url, d.text);
  while (captured.size > 40) captured.delete(captured.keys().next().value!);
});
window.postMessage({ __odm_hls_replay__: 1 }, "*"); // what hook.ts saw before we loaded

/** HLS renditions from captured playlists; also the master URLs they expand. */
function capturedSources(): { sources: VideoSource[]; masters: Set<string> } {
  const sources: VideoSource[] = [];
  const masters = new Set<string>();
  const all = [...captured];
  const medias = all.filter(([, t]) => !isHlsMaster(t));
  const loaded = new Set(medias.map(([u]) => u));
  const duration = Math.max(0, ...medias.map(([, t]) => hlsDuration(t)));
  const variants = new Set<string>();
  for (const [murl, text] of all.filter(([, t]) => isHlsMaster(t))) {
    masters.add(murl);
    for (const v of hlsVariants(text, murl)) {
      variants.add(v.url);
      sources.push({
        url: v.url,
        kind: "hls",
        height: v.height,
        label: v.height ? `${v.height}p · HLS` : "HLS",
        playing: loaded.has(v.url),
        estimate: v.bandwidth && duration ? Math.round((v.bandwidth / 8) * duration) : undefined,
      });
    }
  }
  // A media playlist without a master we saw (or an audio-only one).
  for (const [u] of medias) if (!variants.has(u)) sources.push({ url: u, kind: "hls", label: "HLS", playing: true });
  return { sources, masters };
}

/** The playlist text for an HLS source: as captured, else fetched like the player would. */
async function playlistFor(url: string): Promise<string | undefined> {
  const have = captured.get(url);
  if (have) return have;
  try {
    const r = await fetch(url, { credentials: "include" });
    const t = await r.text();
    if (r.ok && t.replace(/^﻿/, "").startsWith("#EXTM3U")) return t;
  } catch {
    /* cross-origin without CORS, expired token, ...: ODM will try the URL itself */
  }
  return undefined;
}

// ---- collecting sources ----

function rawFromVideo(v: HTMLVideoElement): RawSource[] {
  const out: RawSource[] = [];
  v.querySelectorAll("source").forEach((s) => {
    const src = s.getAttribute("src");
    if (!src) return;
    out.push({
      url: src,
      type: s.type,
      label: s.getAttribute("label") || s.getAttribute("title") || s.dataset.quality || undefined,
      // Players disagree on the attribute name for the height.
      height: s.getAttribute("height") ?? s.getAttribute("res") ?? s.getAttribute("size") ?? s.dataset.res ?? undefined,
    });
  });
  // Last, so a labelled <source> with the same URL keeps its nicer label.
  // v.src of an empty src="" attribute is the page address, not a video.
  const cur = v.currentSrc || (v.getAttribute("src") ? v.src : "");
  if (cur) out.push({ url: cur, height: v.videoHeight || undefined, width: v.videoWidth || undefined });
  return out;
}

/** Player configs (JSON in attributes and json scripts) in a document. */
function configsIn(doc: Document): string[] {
  const out: string[] = [];
  doc.querySelectorAll("*").forEach((node) => {
    for (let i = 0; i < node.attributes.length; i++) {
      const v = node.attributes[i].value;
      if (mayHoldMedia(v)) out.push(v);
    }
  });
  doc.querySelectorAll('script[type*="json"]').forEach((s) => mayHoldMedia(s.textContent) && out.push(s.textContent));
  return out;
}

// Players often consume their config attribute and remove it (Show TV's
// data-hope-video is gone once the player starts), so the page's own HTML is
// read once as well. Same origin, same cookies; parsed inertly (DOMParser
// runs no scripts and loads nothing).
let pageConfigs: Promise<string[]> | null = null;
function rawPageConfigs(): Promise<string[]> {
  pageConfigs ??= fetch(location.href, { credentials: "include" })
    .then((r) => (r.ok && /html/i.test(r.headers.get("content-type") ?? "") ? r.text() : ""))
    .then((html) => (html ? configsIn(new DOMParser().parseFromString(html, "text/html")) : []))
    .catch(() => []);
  return pageConfigs;
}

/** The video's name for file names: og:title, else the document title. */
function pageTitle(): string {
  const og = document.querySelector('meta[property="og:title"]')?.getAttribute("content");
  return (og || document.title || "").trim();
}

function collect(v: HTMLVideoElement, extraConfigs: string[] = []): VideoGroup[] {
  const base = location.href;
  const conv = (xs: RawSource[]) => xs.map((x) => toSource(x, base)).filter((s): s is VideoSource => s !== null);
  const groups: VideoGroup[] = [];

  // Player configs first: they know every quality, not just the one playing
  // (an HLS player's <video> only shows blob:). Scanned on menu open only.
  for (const c of [...configsIn(document), ...extraConfigs].slice(0, 80)) groups.push(...jsonSources(c, base));
  // What the player actually streams: the variants of the playlists it loaded.
  const hls = capturedSources();
  groups.push({ title: t("vid.thisVideo"), sources: [...hls.sources, ...conv(rawFromVideo(v))], generic: true });

  const others: RawSource[] = [];
  document.querySelectorAll("video").forEach((o) => o !== v && !looksLikeAd(o) && others.push(...rawFromVideo(o)));
  if (window === window.top) {
    const metas = 'meta[property="og:video"],meta[property="og:video:url"],meta[property="og:video:secure_url"],' +
      'meta[property="twitter:player:stream"],meta[name="twitter:player:stream"]';
    document.querySelectorAll(metas).forEach((m) => {
      const c = m.getAttribute("content");
      // og:video is often an embed *page*; only take real media files.
      if (c && isMediaUrl(c)) others.push({ url: c });
    });
  }
  groups.push({ title: t("vid.pageVideos"), sources: conv(others), generic: true });

  // A master playlist is listed as its variants instead ("Auto · HLS" -> 1080p,
  // 720p, ...). Players often add a query (tokens, cache busters) when loading it.
  const bare = (u: string) => u.split(/[?#]/)[0];
  const masters = new Set([...hls.masters].map(bare));
  for (const g of groups) g.sources = g.sources.filter((s) => s.kind !== "hls" || s.playing || !masters.has(bare(s.url)));

  let playing = "";
  try {
    playing = new URL(v.currentSrc || v.src, base).href;
  } catch {
    /* no source */
  }
  const merged = mergeGroups(groups, playing);
  // YouTube & co. stream in pieces (MSE) that only yt-dlp can follow: offer the
  // page itself to ODM's yt-dlp. On other sites only when nothing else was found.
  if (isYtdlpSite(location.hostname) || !merged.some((g) => g.sources.some((s) => s.kind !== "stream"))) {
    const presets: [number, string][] = [[0, t("vid.best")], [1080, "1080p"], [720, "720p"], [480, "480p"], [-1, t("vid.audio")]];
    merged.push({
      title: t("vid.ytdlp"),
      generic: true,
      sources: presets.map(([height, label]) => ({ url: location.href, label, kind: "ytdlp", height })),
    });
  }
  return merged;
}

// ---- menu ----

const sizes = new Map<string, number>(); // 0 = asked, unknown
let sizeEls = new Map<string, HTMLElement>();

function openMenu() {
  if (!target || !ui) return;
  const v = target;
  render(v, collect(v));
  ui.menu.hidden = false;
  menuOpen = true;
  clearTimeout(hideTimer);
  place();
  // The page HTML arrives a moment later; redraw if it knew more.
  void rawPageConfigs().then((extra) => {
    if (!extra.length || !menuOpen || target !== v) return;
    const more = collect(v, extra);
    if (more.flatMap((g) => g.sources).length > shown) render(v, more);
  });
}

let shown = 0; // sources in the menu right now

let lastOffered = "";
function render(v: HTMLVideoElement, groups: VideoGroup[]) {
  const menu = ui!.menu;
  menu.replaceChildren();
  sizeEls = new Map();
  shown = groups.flatMap((g) => g.sources).length;
  // ODM's log (when on): what the menu offered for this video. Once per
  // distinct menu: it re-renders when the page's own config arrives.
  const offered =
    groups.map((g) => `${g.title}: ${g.sources.map((s) => `${s.kind} ${s.label} ${s.url.slice(0, 200)}`).join(" | ")}`).join(" || ") || "nothing";
  if (offered !== lastOffered) {
    lastOffered = offered;
    void ext.runtime
      .sendMessage({
        type: "odm-log",
        text: "video menu",
        fields: { page: location.href, videoSrc: (v.currentSrc || v.getAttribute("src") || "").slice(0, 300), offered },
      })
      .catch(() => {});
  }
  if (!groups.length) {
    const mse = (v.currentSrc || "").startsWith("blob:");
    menu.append(el("div", "note", mse ? t("vid.mse") : t("vid.none")));
  }
  // A player config's title ("Saatini satacaktı dolandırıldı!") beats the
  // page title ("... | Show Özel Haber") for the generic groups too.
  const named = groups.find((g) => !g.generic && g.title)?.title;
  for (const g of groups) {
    if (groups.length > 1 && g.title) menu.append(el("div", "head", g.title));
    // yt-dlp pages: the live document title (single-page sites like YouTube
    // leave og:title at the first video).
    const name = g.sources[0]?.kind === "ytdlp" ? cleanTitle(document.title) : g.generic || !g.title ? (named ?? pageTitle()) : g.title;
    for (const s of g.sources) menu.append(item(s, name));
  }
  place();
  void fillSizes(groups.flatMap((g) => g.sources).filter((s) => s.kind === "file"));
}

function closeMenu() {
  if (!ui || !menuOpen) return;
  ui.menu.hidden = true;
  menuOpen = false;
}

function item(s: VideoSource, title: string): HTMLElement {
  const b = el("button", "item") as HTMLButtonElement;
  b.type = "button";
  b.title = s.url;
  b.append(el("span", "q", s.label));
  if (s.playing) b.append(el("span", "tag", t("vid.playing")));
  const size = el("span", "size");
  b.append(size);
  if (s.kind === "stream") {
    b.disabled = true;
    size.textContent = t("vid.stream");
  } else if (s.kind === "ytdlp") {
    b.addEventListener("click", () => void pick(s, fileNameFor(title, s)));
  } else if (s.kind === "hls") {
    if (s.estimate) size.textContent = "~" + human(s.estimate);
    b.addEventListener("click", () => void pick(s, fileNameFor(title, s)));
  } else {
    const n = sizes.get(s.url);
    if (n) size.textContent = human(n);
    sizeEls.set(s.url, size);
    b.addEventListener("click", () => void pick(s, fileNameFor(title, s)));
  }
  return b;
}

async function fillSizes(list: VideoSource[]) {
  const want = list.map((s) => s.url).filter((u) => !sizes.has(u));
  if (want.length) {
    let got: Record<string, number> | undefined;
    try {
      got = (await ext.runtime.sendMessage({ type: "odm-video-sizes", urls: want })) as typeof got;
    } catch {
      /* background asleep or gone: sizes stay blank */
    }
    for (const u of want) sizes.set(u, got?.[u] ?? 0);
  }
  for (const [u, e] of sizeEls) {
    const n = sizes.get(u);
    if (n) e.textContent = human(n);
  }
}

async function pick(s: VideoSource, filename: string) {
  closeMenu();
  try {
    // HLS: send the playlist itself; its URL may only work in this session.
    const playlist = s.kind === "hls" ? await playlistFor(s.url) : undefined;
    const r = (await ext.runtime.sendMessage({
      type: "odm-video",
      url: s.url,
      filename,
      referer: location.href,
      size: (s.kind === "hls" ? s.estimate : s.kind === "file" ? sizes.get(s.url) : 0) || undefined,
      media: s.kind === "hls" || s.kind === "ytdlp" ? s.kind : undefined,
      playlist,
      format: s.kind === "ytdlp" ? ytFormat(s.height ?? 0) : undefined,
    })) as { ok: boolean; error?: string } | undefined;
    if (r && !r.ok) throw new Error(r.error ?? "?");
    flash(t("vid.sent"), true);
  } catch (e) {
    flash(t("vid.failed", { err: (e as Error).message }), false);
  }
}

function flash(text: string, ok: boolean) {
  if (!ui) return;
  const { button, label } = ui;
  clearTimeout(flashTimer);
  label.textContent = text;
  button.classList.toggle("ok", ok);
  button.classList.toggle("bad", !ok);
  hideLater(IDLE_HIDE_MS);
  flashTimer = window.setTimeout(() => {
    label.textContent = t("bg.menu");
    button.classList.remove("ok", "bad");
  }, 2500);
}
