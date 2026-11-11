import {
  clientFor,
  ext,
  loadSettings,
  localClient,
  remoteOf,
  report,
  scratch,
  targetName,
  type DownloadRequest,
  type Hints,
  type Pending,
  type Settings,
} from "./api";
import { t } from "./i18n";
import { dispositionName } from "./util";

const CAPTURE_SCHEMES = ["http:", "https:", "ftp:"];

// Settings are cached so the capture decision does not wait on storage: the
// browser may finish a small file before we get to cancel it.
let cached: Promise<Settings> | null = null;
// Resolved copy for listeners that must decide synchronously (blocking webRequest).
let current: Settings | null = null;
const settings = () =>
  (cached ??= loadSettings().then((s) => {
    current = s;
    return s;
  }));
ext.storage.onChanged.addListener((changes) => {
  cached = null;
  void settings().then(() => {
    if (changes.language) ext.contextMenus.update("odm-link", { title: t("bg.menu") }).catch(() => {});
  });
});
void settings();

// Tell a running ODM which extension version this browser has (shown in
// Integrations). Never starts ODM: that would happen on every browser start.
localClient()
  .info()
  .catch(() => {});

// URLs the webRequest path already handed to ODM; onCreated must not take them again.
const taken = new Map<string, number>();
function recentlyTaken(...urls: string[]): boolean {
  const now = Date.now();
  for (const [u, exp] of taken) if (exp < now) taken.delete(u);
  return urls.some((u) => taken.has(u));
}

// URLs we deliberately gave back to the browser; the listener must not capture them again.
const skip = new Map<string, number>();
function isSkipped(...urls: string[]): boolean {
  const now = Date.now();
  for (const [u, exp] of skip) if (exp < now) skip.delete(u);
  return urls.some((u) => skip.has(u));
}
async function browserDownload(url: string, filename?: string) {
  skip.set(url, Date.now() + 60_000); // registered before the call: onCreated may fire first
  await ext.downloads.download(filename ? { url, filename } : { url });
}

async function cookieHeader(url: string): Promise<string> {
  try {
    const cs = await ext.cookies.getAll({ url });
    return cs.map((c) => `${c.name}=${c.value}`).join("; ");
  } catch {
    return ""; // no cookie permission: still fine for public downloads
  }
}

async function buildRequest(url: string, referer?: string): Promise<DownloadRequest> {
  return {
    url,
    referer: referer || undefined,
    cookies: (await cookieHeader(url)) || undefined,
    userAgent: navigator.userAgent,
  };
}

/** Stop the browser's own copy and remove whatever it already wrote. */
async function takeFromBrowser(id: number) {
  const steps: [string, () => Promise<unknown>][] = [
    ["cancel", () => ext.downloads.cancel(id)],
    ["removeFile", () => ext.downloads.removeFile(id)],
    ["erase", () => ext.downloads.erase({ id })],
  ];
  const failed: string[] = [];
  for (const [name, step] of steps) {
    try {
      await step();
      console.log(`[ODM] takeFromBrowser #${id}: ${name} ok`);
    } catch (e) {
      // Often harmless (cancel on a finished item, removeFile with nothing on
      // disk), but logged: swallowing these hid the Firefox double-download bug.
      console.log(`[ODM] takeFromBrowser #${id}: ${name} failed: ${(e as Error)?.message ?? e}`);
      failed.push(`${name}: ${(e as Error)?.message ?? e}`);
    }
  }
  return failed;
}

// Firefox only: blocking webRequest stops a download before the browser creates
// it. downloads.onCreated fires too late there: a ~100 KB file is already saved
// by the time cancel() runs, so both Firefox and ODM ended up with a copy.
// Chrome MV3 has no blocking webRequest (and no permission for it), so
// ext.webRequest is undefined there and Chrome keeps the onCreated path.

// Types Firefox shows in the tab itself; anything else is saved as a file.
function rendersInline(type: string): boolean {
  if (!type) return true; // unknown: Firefox sniffs; leave it alone
  if (/^(text|image|audio|video)\//.test(type)) return true;
  return [
    "application/pdf",
    "application/json",
    "application/xml",
    "application/xhtml+xml",
    "application/javascript",
    "application/x-javascript",
    "application/ecmascript",
    "application/rss+xml",
    "application/atom+xml",
    "application/manifest+json",
    "application/ld+json",
    "multipart/x-mixed-replace",
  ].includes(type);
}

type Header = { name: string; value?: string };
const header = (hs: Header[] | undefined, name: string) =>
  hs?.find((h) => h.name.toLowerCase() === name)?.value ?? "";

function isDownloadResponse(hs: Header[] | undefined): boolean {
  if (/^\s*attachment/i.test(header(hs, "content-disposition"))) return true;
  const type = header(hs, "content-type").split(";")[0].trim().toLowerCase();
  return !rendersInline(type);
}

function hintsFromHeaders(hs: Header[] | undefined): Hints {
  const size = Number(header(hs, "content-length"));
  return {
    filename: dispositionName(header(hs, "content-disposition")) || undefined,
    size: size > 0 ? size : undefined,
    mime: header(hs, "content-type").split(";")[0].trim() || undefined,
    resumable: /bytes/i.test(header(hs, "accept-ranges")) || undefined,
  };
}

interface HeadersDetails {
  url: string;
  method: string;
  statusCode: number;
  responseHeaders?: Header[];
  originUrl?: string; // Firefox: page that started the request
  documentUrl?: string;
}

type BlockingResponse = { cancel?: boolean };

function onHeaders(d: HeadersDetails): BlockingResponse {
  const s = current;
  if (!s || !s.enabled) return {};
  if (d.method !== "GET" || d.statusCode !== 200) return {}; // 3xx: wait for the final hop
  if (isSkipped(d.url)) return {};
  let scheme: string;
  try {
    scheme = new URL(d.url).protocol;
  } catch {
    return {};
  }
  if (!CAPTURE_SCHEMES.includes(scheme)) return {};
  if (!isDownloadResponse(d.responseHeaders)) return {};
  const hints = hintsFromHeaders(d.responseHeaders);
  if (s.minSizeMB > 0 && hints.size && hints.size < s.minSizeMB * 1024 * 1024) return {};

  taken.set(d.url, Date.now() + 10_000);
  console.log(`[ODM] webRequest took ${d.url}`);
  // Hand off after returning: starting ODM or opening the dialog takes time
  // and the request must not hang meanwhile.
  void (async () => {
    try {
      await handOff(await buildRequest(d.url, d.originUrl || d.documentUrl), hints, s);
    } catch (e) {
      await report(false, t("bg.error", { err: (e as Error).message }));
    }
  })();
  return { cancel: true };
}

const webRequest = (ext as unknown as {
  webRequest?: {
    onHeadersReceived: {
      addListener(
        cb: (d: HeadersDetails) => BlockingResponse | Promise<BlockingResponse>,
        filter: object,
        extra: string[],
      ): void;
    };
  };
}).webRequest;
if (webRequest?.onHeadersReceived) {
  try {
    webRequest.onHeadersReceived.addListener(
      // The Firefox background page is suspended when idle; a click can wake it
      // before settings are loaded. Firefox accepts a promise here, so wait for
      // them instead of letting the browser have the file.
      (d) => (current ? onHeaders(d) : settings().then(() => (current ? onHeaders(d) : {}), () => ({}))),
      // Only page navigations: our own fetch() calls are xmlhttprequest and
      // downloads.download() is "other", so neither can loop back here.
      { urls: ["<all_urls>"], types: ["main_frame", "sub_frame"] },
      ["blocking", "responseHeaders"],
    );
    console.log("[ODM] blocking webRequest capture active");
  } catch (e) {
    console.log(`[ODM] blocking webRequest unavailable: ${(e as Error).message}`);
  }
}

/**
 * Where does it go? By default the download dialog asks (name, folder, this
 * computer or the server) before anything starts. With asking turned off it
 * goes straight to the default destination.
 */
async function handOff(req: DownloadRequest, hints: Hints, s: Settings) {
  if (s.askBeforeDownload) {
    await openDialog(req, hints);
    return;
  }
  const target = s.defaultTarget === "remote" && remoteOf(s) ? "remote" : "local";
  try {
    await (await clientFor(target, s)).add(req, { filename: hints.filename });
    await report(true, t("bg.sent", { where: targetName(target, s), url: req.url }));
  } catch (e) {
    if (target === "remote") {
      await report(false, (e as Error).message);
      return;
    }
    await report(false, t("bg.giveBack", { err: (e as Error).message }));
    await browserDownload(req.url);
  }
}

async function openDialog(req: DownloadRequest, hints: Hints) {
  const id = crypto.randomUUID();
  const key = `pending:${id}`;
  await scratch.set({ [key]: { req, hints } satisfies Pending });
  const win = await ext.windows.create({
    url: ext.runtime.getURL(`download.html?id=${id}`),
    type: "popup",
    width: 540,
    height: 600,
  });
  await scratch.set({ [key]: { req, hints, windowId: win.id } satisfies Pending });
}

// Closing the dialog without picking cancels the download: say so instead of
// letting it vanish silently. (A pick removes the pending record first.)
ext.windows.onRemoved.addListener(async (windowId) => {
  const all = await scratch.get(null);
  for (const [key, value] of Object.entries(all)) {
    if (!key.startsWith("pending:") || (value as Pending).windowId !== windowId) continue;
    await scratch.remove(key);
    await report(false, t("bg.cancelled", { url: (value as Pending).req.url }));
  }
});

const baseName = (p: string) => p.split(/[\\/]/).pop() ?? "";

ext.downloads.onCreated.addListener(async (item) => {
  try {
    const url = item.finalUrl || item.url;
    if (isSkipped(item.url, url)) return;
    if (recentlyTaken(item.url, url)) {
      // webRequest already cancelled and handed it off; if Firefox still made
      // an item, just remove it.
      await takeFromBrowser(item.id);
      return;
    }
    const s = await settings();
    if (!s.enabled) return;
    let scheme: string;
    try {
      scheme = new URL(url).protocol;
    } catch {
      return;
    }
    if (!CAPTURE_SCHEMES.includes(scheme)) return; // blob:, data:, etc. stay in the browser
    const size = item.fileSize > 0 ? item.fileSize : item.totalBytes > 0 ? item.totalBytes : 0;
    if (s.minSizeMB > 0 && size > 0 && size < s.minSizeMB * 1024 * 1024) return;

    // Cancel first, before any network call; otherwise a small file is already
    // saved by the time ODM answers and you end up with two copies.
    console.log(`[ODM] onCreated #${item.id} state=${item.state} ${url}`);
    const failed = await takeFromBrowser(item.id);
    // cancel fails when the browser already finished; then only removeFile keeps
    // us from leaving a second copy, so say so if that failed too.
    if (failed.some((f) => f.startsWith("cancel")) && failed.some((f) => f.startsWith("removeFile"))) {
      await report(false, t("bg.kept", { why: failed.join("; ") }));
    }
    const hints: Hints = { filename: baseName(item.filename) || undefined, size: size || undefined, mime: item.mime || undefined };
    await handOff(await buildRequest(url, item.referrer), hints, s);
  } catch (e) {
    await report(false, t("bg.error", { err: (e as Error).message }));
  }
});

ext.runtime.onMessage.addListener((msg: { type?: string; url?: string; filename?: string }, _sender, sendResponse) => {
  if (msg?.type !== "browser-download" || !msg.url) return false;
  browserDownload(msg.url, msg.filename).then(
    () => sendResponse({ ok: true }),
    (e: Error) => sendResponse({ ok: false, error: e.message }),
  );
  return true; // async response
});

ext.runtime.onInstalled.addListener(async () => {
  await settings();
  ext.contextMenus.create({ id: "odm-link", title: t("bg.menu"), contexts: ["link"] });
});

ext.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "odm-link" || !info.linkUrl) return;
  try {
    await handOff(await buildRequest(info.linkUrl, tab?.url), {}, await settings());
  } catch (e) {
    await report(false, t("bg.error", { err: (e as Error).message }));
  }
});
