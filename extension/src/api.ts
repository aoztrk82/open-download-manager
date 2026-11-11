// Shared code for the ODM browser extension: API client, settings, launching the
// local app, and status reporting. Same HTTP protocol the Go CLI and Web UI use.

// Firefox exposes a promise-based `browser` namespace; its `chrome` namespace is
// callback-based there, so `await chrome.storage.local.get()` can yield undefined.
// Chrome has no `browser`, and its `chrome.*` calls return promises in MV3.
export const ext: typeof chrome =
  (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome;

export const LOCAL_URL = "http://127.0.0.1:6800";

/** Which browser family this build runs in; Edge uses the Chrome build. */
export function browserId(): "firefox" | "chrome" | "edge" {
  if (ext.runtime.getURL("").startsWith("moz-extension:")) return "firefox";
  return / Edg\//.test(navigator.userAgent) ? "edge" : "chrome";
}
export const extVersion = () => ext.runtime.getManifest().version;

export interface Info {
  version: string;
  defaultDir: string;
  /** Extension versions bundled in odm.exe by family; missing on older daemons. */
  extensions?: Record<string, string>;
}

/**
 * The extension is installed from odm.exe (ODM > Integrations), so its version
 * should match the one ODM bundles. Returns a warning when it does not.
 */
export function versionWarning(info: Info): string {
  const fam = browserId() === "firefox" ? "firefox" : "chrome";
  const bundled = info.extensions?.[fam];
  if (!bundled || bundled === extVersion()) return "";
  return `Extension ${extVersion()} does not match ODM ${info.version} (bundles ${bundled}). ` +
    "Update it from ODM: Integrations > Browser Extensions.";
}
export const NATIVE_HOST = "odm_host"; // registered by odm.exe on its first run

export interface Settings {
  enabled: boolean;
  /** Optional remote ODM server. Empty (or a loopback URL) means "local only". */
  serverUrl: string;
  token: string;
  minSizeMB: number;
}

export const DEFAULTS: Settings = {
  enabled: true,
  serverUrl: "",
  token: "",
  minSizeMB: 0,
};

export interface Remote {
  url: string;
  token: string;
  label: string;
}

/** The configured remote server, or null when only the local ODM should be used. */
export function remoteOf(s: Settings): Remote | null {
  const raw = (s.serverUrl ?? "").trim();
  if (!raw) return null;
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return null;
  }
  // Older versions stored the local daemon here; that is not a remote server.
  if (["127.0.0.1", "localhost", "[::1]"].includes(u.hostname)) return null;
  return { url: raw.replace(/\/+$/, ""), token: s.token, label: u.host };
}

export interface DownloadRequest {
  url: string;
  headers?: Record<string, string>;
  cookies?: string;
  referer?: string;
  userAgent?: string;
}

export interface Download {
  id: string;
  request: DownloadRequest;
  filename: string;
  status: "queued" | "active" | "paused" | "completed" | "failed";
  size: number;
  downloaded: number;
  error?: string;
}

export class OdmClient {
  private base: string;

  constructor(base: string, private token = "") {
    this.base = base.replace(/\/+$/, "");
  }

  private async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.token) headers.Authorization = `Bearer ${this.token}`; // local daemon needs none
    const r = await fetch(this.base + "/v1" + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!r.ok) {
      const e = await r.json().catch(() => ({}));
      throw new Error((e as { error?: string }).error ?? `${r.status} ${r.statusText}`);
    }
    return r.status === 204 ? (undefined as T) : ((await r.json()) as T);
  }

  // ?ext= lets ODM show which extension version each browser runs.
  info() {
    return this.call<Info>("GET", `/info?ext=${encodeURIComponent(`${browserId()}/${extVersion()}`)}`);
  }
  list() { return this.call<Download[]>("GET", "/downloads"); }
  add(request: DownloadRequest, filename?: string) {
    return this.call<Download>("POST", "/downloads", { request, filename });
  }
}

export const localClient = () => new OdmClient(LOCAL_URL);

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * Returns a client for the local ODM, starting the app first if it is closed.
 * A web extension cannot launch programs, so this asks the native-messaging host
 * (odm.exe itself) to do it.
 */
export async function ensureLocal(): Promise<OdmClient> {
  const c = localClient();
  try {
    await c.info();
    return c;
  } catch {
    /* not running: try to start it */
  }
  let reply: { ok?: boolean; error?: string } | undefined;
  try {
    reply = (await ext.runtime.sendNativeMessage(NATIVE_HOST, { cmd: "start" })) as typeof reply;
  } catch (e) {
    throw new Error(
      "ODM is not running and could not be started automatically. Start odm.exe once " +
        `(it registers the browser integration), then try again. (${(e as Error).message})`,
    );
  }
  if (reply && reply.ok === false) throw new Error(`ODM could not be started: ${reply.error ?? "unknown error"}`);
  for (let i = 0; i < 40; i++) {
    await sleep(250);
    try {
      await c.info();
      return c;
    } catch {
      /* still starting */
    }
  }
  throw new Error("ODM was started but is not answering on " + LOCAL_URL);
}

export async function loadSettings(): Promise<Settings> {
  const s = await ext.storage.local.get({ ...DEFAULTS });
  return s as Settings;
}

/** Temporary hand-off data between the background script and the chooser window. */
export const scratch = ext.storage.session ?? ext.storage.local;

export interface Pending {
  req: DownloadRequest;
  windowId?: number;
}

/**
 * Visible feedback: toolbar badge + the "last event" line in the popup. Without
 * this a failure is silent and the user just sees the browser doing something else.
 */
export async function report(ok: boolean, msg: string) {
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
