// Video button. Hovering a video shows "Download with ODM" in its top-right
// corner; the menu lists every rendition this page reveals (site playlists,
// <source> tags, the playing URL, other videos, og:video) and hands the picked
// one to the background script, which opens the usual download dialog.
// Runs in every http(s) frame, so it must stay cheap until a video is hovered.
import { ext } from "./ext";
import { setLang, t } from "./i18n";
import {
  archivePlaylist,
  human,
  isMediaUrl,
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

function videoAt(x: number, y: number, root: Document | ShadowRoot = document, seen = new Set<Node>()): HTMLVideoElement | null {
  seen.add(root);
  // All stacked elements, not just the top one: players cover the <video> with controls.
  for (const el of root.elementsFromPoint(x, y)) {
    if (el === host) continue;
    if (el.localName === "video") return el as HTMLVideoElement;
    const sr = shadowOf(el);
    if (sr && !seen.has(sr)) {
      const v = videoAt(x, y, sr, seen);
      if (v) return v;
    }
  }
  return null;
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

let lastMove = 0;
document.addEventListener(
  "mousemove",
  (e) => {
    if (!enabled) return;
    const now = performance.now();
    if (now - lastMove < 120) return; // elementsFromPoint is not free; 8 checks a second is plenty
    lastMove = now;
    const v = videoAt(e.clientX, e.clientY);
    if (v && bigEnough(v.getBoundingClientRect())) {
      show(v);
      hideLater(IDLE_HIDE_MS);
    } else if (target && !menuOpen) hideLater(LEAVE_HIDE_MS);
  },
  { capture: true, passive: true },
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
  const cur = v.currentSrc || v.src;
  if (cur) out.push({ url: cur, height: v.videoHeight || undefined, width: v.videoWidth || undefined });
  return out;
}

function collect(v: HTMLVideoElement): VideoGroup[] {
  const base = location.href;
  const conv = (xs: RawSource[]) => xs.map((x) => toSource(x, base)).filter((s): s is VideoSource => s !== null);
  const groups: VideoGroup[] = [];

  // Site playlists first: they know every quality, not just the one playing.
  if (/(^|\.)archive\.org$/.test(location.hostname)) {
    document.querySelectorAll("play-av[playlist]").forEach((p) => groups.push(...archivePlaylist(p.getAttribute("playlist") ?? "", base)));
  }
  groups.push({ title: t("vid.thisVideo"), sources: conv(rawFromVideo(v)) });

  const others: RawSource[] = [];
  document.querySelectorAll("video").forEach((o) => o !== v && others.push(...rawFromVideo(o)));
  if (window === window.top) {
    const metas = 'meta[property="og:video"],meta[property="og:video:url"],meta[property="og:video:secure_url"],' +
      'meta[property="twitter:player:stream"],meta[name="twitter:player:stream"]';
    document.querySelectorAll(metas).forEach((m) => {
      const c = m.getAttribute("content");
      // og:video is often an embed *page*; only take real media files.
      if (c && isMediaUrl(c)) others.push({ url: c });
    });
  }
  groups.push({ title: t("vid.pageVideos"), sources: conv(others) });

  let playing = "";
  try {
    playing = new URL(v.currentSrc || v.src, base).href;
  } catch {
    /* no source */
  }
  return mergeGroups(groups, playing);
}

// ---- menu ----

const sizes = new Map<string, number>(); // 0 = asked, unknown
let sizeEls = new Map<string, HTMLElement>();

function openMenu() {
  if (!target || !ui) return;
  const groups = collect(target);
  const menu = ui.menu;
  menu.replaceChildren();
  sizeEls = new Map();
  if (!groups.length) {
    const mse = (target.currentSrc || "").startsWith("blob:");
    menu.append(el("div", "note", mse ? t("vid.mse") : t("vid.none")));
  }
  for (const g of groups) {
    if (groups.length > 1 && g.title) menu.append(el("div", "head", g.title));
    for (const s of g.sources) menu.append(item(s));
  }
  menu.hidden = false;
  menuOpen = true;
  clearTimeout(hideTimer);
  place();
  void fillSizes(groups.flatMap((g) => g.sources).filter((s) => s.kind === "file"));
}

function closeMenu() {
  if (!ui || !menuOpen) return;
  ui.menu.hidden = true;
  menuOpen = false;
}

function item(s: VideoSource): HTMLElement {
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
  } else {
    const n = sizes.get(s.url);
    if (n) size.textContent = human(n);
    sizeEls.set(s.url, size);
    b.addEventListener("click", () => void pick(s));
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

async function pick(s: VideoSource) {
  closeMenu();
  try {
    const r = (await ext.runtime.sendMessage({
      type: "odm-video",
      url: s.url,
      referer: location.href,
      size: sizes.get(s.url) || undefined,
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
