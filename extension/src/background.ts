import { clientFromSettings, loadSettings, type DownloadRequest } from "./api";

const CAPTURE_SCHEMES = ["http:", "https:", "ftp:"];

async function cookieHeader(url: string): Promise<string> {
  try {
    const cs = await chrome.cookies.getAll({ url });
    return cs.map((c) => `${c.name}=${c.value}`).join("; ");
  } catch {
    return "";
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

function setBadge(text: string, color: string) {
  chrome.action.setBadgeBackgroundColor({ color });
  chrome.action.setBadgeText({ text });
  setTimeout(() => chrome.action.setBadgeText({ text: "" }), 3000);
}

// Send to ODM first; only take the download away from the browser on success,
// so an unreachable daemon never makes a download silently vanish.
chrome.downloads.onCreated.addListener(async (item) => {
  const s = await loadSettings();
  if (!s.enabled || !s.token) return;
  const url = item.finalUrl || item.url;
  let scheme: string;
  try {
    scheme = new URL(url).protocol;
  } catch {
    return;
  }
  if (!CAPTURE_SCHEMES.includes(scheme)) return; // blob:, data:, etc. stay in the browser
  if (s.minSizeMB > 0 && item.fileSize > 0 && item.fileSize < s.minSizeMB * 1024 * 1024) return;

  try {
    const odm = await clientFromSettings();
    await odm.add(await buildRequest(url, item.referrer));
  } catch (e) {
    console.warn("ODM unreachable, leaving download to the browser:", e);
    setBadge("!", "#dc2626");
    return;
  }
  try {
    await chrome.downloads.cancel(item.id);
    await chrome.downloads.erase({ id: item.id });
  } catch {
    /* already finished/removed: nothing to do */
  }
  setBadge("OK", "#16a34a");
});

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: "odm-link",
    title: "Download with ODM",
    contexts: ["link"],
  });
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "odm-link" || !info.linkUrl) return;
  try {
    const odm = await clientFromSettings();
    await odm.add(await buildRequest(info.linkUrl, tab?.url));
    setBadge("OK", "#16a34a");
  } catch (e) {
    console.warn("ODM add failed:", e);
    setBadge("!", "#dc2626");
  }
});
