import {
  ensureLocal,
  ext,
  loadSettings,
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
const settings = () => (cached ??= loadSettings());
ext.storage.onChanged.addListener(() => {
  cached = null;
});
void settings();

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
  for (const step of [
    () => ext.downloads.cancel(id),
    () => ext.downloads.removeFile(id),
    () => ext.downloads.erase({ id }),
  ]) {
    try {
      await step();
    } catch {
      /* already finished, gone or nothing on disk yet */
    }
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
    await takeFromBrowser(item.id);
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
