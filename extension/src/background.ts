import { clientFromSettings, ext, loadSettings, type DownloadRequest } from "./api";

const CAPTURE_SCHEMES = ["http:", "https:", "ftp:"];

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

// Visible feedback: toolbar badge + the "last event" line in the popup. Without
// this a failure is silent and the browser just downloads the file itself.
async function report(ok: boolean, msg: string) {
  console.log(`[ODM] ${ok ? "ok" : "FAILED"}: ${msg}`);
  try {
    await ext.action.setBadgeBackgroundColor({ color: ok ? "#16a34a" : "#dc2626" });
    await ext.action.setBadgeText({ text: ok ? "OK" : "!" });
    setTimeout(() => ext.action.setBadgeText({ text: "" }), 4000);
    await ext.storage.local.set({ lastEvent: { t: Date.now(), ok, msg } });
  } catch {
    /* reporting must never break capturing */
  }
}

// Send to ODM first; only take the download away from the browser on success,
// so an unreachable daemon never makes a download silently vanish.
ext.downloads.onCreated.addListener(async (item) => {
  try {
    const s = await loadSettings();
    if (!s.enabled) return;
    const url = item.finalUrl || item.url;
    let scheme: string;
    try {
      scheme = new URL(url).protocol;
    } catch {
      return;
    }
    if (!CAPTURE_SCHEMES.includes(scheme)) return; // blob:, data:, etc. stay in the browser
    if (!s.token) {
      await report(false, "ODM token is not set - open the extension settings.");
      return;
    }
    if (s.minSizeMB > 0 && item.fileSize > 0 && item.fileSize < s.minSizeMB * 1024 * 1024) return;

    try {
      const odm = await clientFromSettings();
      await odm.add(await buildRequest(url, item.referrer));
    } catch (e) {
      await report(false, `Cannot reach ODM (${(e as Error).message}). Is it running? Download left to the browser.`);
      return;
    }
    // Handed over: stop the browser's own copy and remove what it already wrote.
    try {
      await ext.downloads.cancel(item.id);
    } catch {
      /* already finished or gone */
    }
    try {
      await ext.downloads.removeFile(item.id);
    } catch {
      /* nothing on disk yet */
    }
    try {
      await ext.downloads.erase({ id: item.id });
    } catch {
      /* already erased */
    }
    await report(true, `Sent to ODM: ${url}`);
  } catch (e) {
    await report(false, `Capture error: ${(e as Error).message}`);
  }
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
    const odm = await clientFromSettings();
    await odm.add(await buildRequest(info.linkUrl, tab?.url));
    await report(true, `Sent to ODM: ${info.linkUrl}`);
  } catch (e) {
    await report(false, `Cannot reach ODM (${(e as Error).message}).`);
  }
});
