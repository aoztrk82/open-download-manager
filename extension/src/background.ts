import {
  ensureLocal,
  ext,
  loadSettings,
  localClient,
  remoteOf,
  report,
  scratch,
  type DownloadRequest,
  type Pending,
  type Settings,
} from "./api";

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
ext.storage.onChanged.addListener(() => {
  cached = null;
  void settings();

// Tell a running ODM which extension version this browser has (shown in
// Integrations). Never starts ODM: that would happen on every browser start.
localClient()
  .info()
  .catch(() => {});
});
void settings();

// URLs the webRequest path already handed to ODM; onCreated must not take them again.
const taken = new Map<string, number>();
function recentlyTaken(...urls: string[]): boolean {
  const now = Date.now();
  for (const [u, t] of taken) if (t < now) taken.delete(u);
  return urls.some((u) => taken.has(u));
}

// URLs we deliberately gave back to the browser; the listener must not capture them again.
const skip = new Map<string, number>();
function isSkipped(...urls: string[]): boolean {
  const now = Date.now();
  for (const [u, t] of skip) if (t < now) skip.delete(u);
  return urls.some((u) => skip.has(u));
}
async function browserDownload(url: string) {
  skip.set(url, Date.now() + 60_000); // registered before the call: onCreated may fire first
  await ext.downloads.download({ url });
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
  const len = Number(header(d.responseHeaders, "content-length"));
  if (s.minSizeMB > 0 && len > 0 && len < s.minSizeMB * 1024 * 1024) return {};

  taken.set(d.url, Date.now() + 10_000);
  console.log(`[ODM] webRequest took ${d.url}`);
  // Hand off after returning: starting ODM or opening the chooser takes time
  // and the request must not hang meanwhile.
  void (async () => {
    try {
      await handOff(await buildRequest(d.url, d.originUrl || d.documentUrl), s);
    } catch (e) {
      await report(false, `Capture error: ${(e as Error).message}`);
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
 * Where does it go? With a remote server configured the user chooses every time
 * (this computer or the server); otherwise it simply downloads locally.
 */
async function handOff(req: DownloadRequest, s: Settings) {
  if (remoteOf(s)) {
    await openChooser(req);
    return;
  }
  try {
    const odm = await ensureLocal();
    await odm.add(req);
    await report(true, `Sent to ODM: ${req.url}`);
  } catch (e) {
    await report(false, `${(e as Error).message} The download was given back to the browser.`);
    await browserDownload(req.url);
  }
}

async function openChooser(req: DownloadRequest) {
  const id = crypto.randomUUID();
  const key = `pending:${id}`;
  await scratch.set({ [key]: { req } satisfies Pending });
  const win = await ext.windows.create({
    url: ext.runtime.getURL(`choose.html?id=${id}`),
    type: "popup",
    width: 480,
    height: 360,
  });
  await scratch.set({ [key]: { req, windowId: win.id } satisfies Pending });
}

// Closing the chooser without picking cancels the download: say so instead of
// letting it vanish silently. (A pick removes the pending record first.)
ext.windows.onRemoved.addListener(async (windowId) => {
  const all = await scratch.get(null);
  for (const [key, value] of Object.entries(all)) {
    if (!key.startsWith("pending:") || (value as Pending).windowId !== windowId) continue;
    await scratch.remove(key);
    await report(false, `Download cancelled: ${(value as Pending).req.url}`);
  }
});

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
    if (s.minSizeMB > 0 && item.fileSize > 0 && item.fileSize < s.minSizeMB * 1024 * 1024) return;

    // Cancel first, before any network call; otherwise a small file is already
    // saved by the time ODM answers and you end up with two copies.
    console.log(`[ODM] onCreated #${item.id} state=${item.state} ${url}`);
    const failed = await takeFromBrowser(item.id);
    // cancel fails when the browser already finished; then only removeFile keeps
    // us from leaving a second copy, so say so if that failed too.
    if (failed.some((f) => f.startsWith("cancel")) && failed.some((f) => f.startsWith("removeFile"))) {
      await report(false, `Browser kept its copy (${failed.join("; ")})`);
    }
    await handOff(await buildRequest(url, item.referrer), s);
  } catch (e) {
    await report(false, `Capture error: ${(e as Error).message}`);
  }
});

ext.runtime.onMessage.addListener((msg: { type?: string; url?: string }, _sender, sendResponse) => {
  if (msg?.type !== "browser-download" || !msg.url) return false;
  browserDownload(msg.url).then(
    () => sendResponse({ ok: true }),
    (e: Error) => sendResponse({ ok: false, error: e.message }),
  );
  return true; // async response
});

ext.runtime.onInstalled.addListener(() => {
  ext.contextMenus.create({
    id: "odm-link",
    title: "Download with ODM",
    contexts: ["link"],
  });
});

ext.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "odm-link" || !info.linkUrl) return;
  try {
    await handOff(await buildRequest(info.linkUrl, tab?.url), await settings());
  } catch (e) {
    await report(false, `Capture error: ${(e as Error).message}`);
  }
});
