// Shared code for the ODM browser extension: API client, settings, launching the
// local app, and status reporting. Same HTTP protocol the Go CLI and Web UI use.
import { ext } from "./ext";
import { setLang, t } from "./i18n";
import { human, type CaptureRules } from "./util";

export { ext };

export const LOCAL_URL = "http://127.0.0.1:6800";
export const NATIVE_HOST = "odm_host"; // registered by odm.exe on its first run

export type Target = "local" | "remote";

export interface Settings {
  enabled: boolean;
  /** Optional remote ODM server. Empty (or a loopback URL) means "local only". */
  serverUrl: string;
  token: string;
  minSizeMB: number;
  /** Show the download dialog (name, size, destination, folder) before each download. */
  askBeforeDownload: boolean;
  /** Where downloads go when the dialog is not shown. */
  defaultTarget: Target;
  /** "auto" (browser language), "en" or "tr". */
  language: string;
  /** "Download with ODM" button over hovered videos (content.ts reads it directly). */
  videoButton: boolean;
  /** Recently used folders per destination; server paths are not local paths. */
  recentDirs: Record<Target, string[]>;
}

export const DEFAULTS: Settings = {
  enabled: true,
  serverUrl: "",
  token: "",
  minSizeMB: 0,
  askBeforeDownload: true,
  defaultTarget: "local",
  language: "auto",
  videoButton: true,
  recentDirs: { local: [], remote: [] },
};

/** Which browser family this build runs in; Edge uses the Chrome build. */
export function browserId(): "firefox" | "chrome" | "edge" {
  if (ext.runtime.getURL("").startsWith("moz-extension:")) return "firefox";
  return / Edg\//.test(navigator.userAgent) ? "edge" : "chrome";
}
export const extVersion = () => ext.runtime.getManifest().version;

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
  /**
   * A stream rather than one file. hls: playlist = the text the player
   * received. ytdlp: url is the page, format a yt-dlp selector.
   */
  media?: { kind: "hls" | "ytdlp"; playlist?: string; format?: string; container?: "mp4" | "mkv" };
}

/** What the browser already told us about a captured download. */
export interface Hints {
  filename?: string;
  size?: number; // bytes, 0/undefined = unknown
  mime?: string;
  resumable?: boolean;
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

export interface Info {
  version: string;
  defaultDir: string;
  canPickFolder?: boolean;
  /** false: a server that only accepts folders inside defaultDir. */
  anyFolder?: boolean;
  /** Extension versions bundled in odm.exe by family; missing on older daemons. */
  extensions?: Record<string, string>;
  /** The daemon can join media pieces (HLS/DASH); missing on older daemons. */
  ffmpeg?: boolean;
  /** The daemon has yt-dlp (YouTube and other sites). */
  ytdlp?: boolean;
  /** Local daemon with a Settings dialog (GET /v1/settings). */
  canSettings?: boolean;
}

export interface Probe {
  size: number; // -1 unknown
  acceptRanges: boolean;
  filename: string;
}

export interface AddOptions {
  filename?: string;
  dir?: string;
  paused?: boolean;
}

/**
 * The extension is installed from odm.exe (ODM > Integrations), so its version
 * should match the one ODM bundles. Returns a warning when it does not.
 */
export function versionWarning(info: Info): string {
  const fam = browserId() === "firefox" ? "firefox" : "chrome";
  const bundled = info.extensions?.[fam];
  if (!bundled || bundled === extVersion()) return "";
  return t("ver.mismatch", { ext: extVersion(), odm: info.version, bundled });
}

export class OdmClient {
  private base: string;

  constructor(base: string, private token = "") {
    this.base = base.replace(/\/+$/, "");
  }

  private async call<T>(method: string, path: string, body?: unknown, timeoutMs = 30_000): Promise<T> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.token) headers.Authorization = `Bearer ${this.token}`; // local daemon needs none
    const r = await fetch(this.base + "/v1" + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(timeoutMs),
    });
    if (!r.ok) {
      const e = await r.json().catch(() => ({}));
      throw new Error((e as { error?: string }).error ?? `${r.status} ${r.statusText}`);
    }
    return r.status === 204 ? (undefined as T) : ((await r.json()) as T);
  }

  // ?ext= lets ODM show which extension version each browser runs.
  info() {
    return this.call<Info>("GET", `/info?ext=${encodeURIComponent(`${browserId()}/${extVersion()}`)}`, undefined, 5_000);
  }
  list() { return this.call<Download[]>("GET", "/downloads"); }
  add(request: DownloadRequest, o: AddOptions = {}) {
    return this.call<Download>("POST", "/downloads", { request, filename: o.filename, dir: o.dir, paused: o.paused });
  }
  /** Adds a line to ODM's download log (only when it keeps one). */
  log(msg: string, fields: Record<string, unknown>) { return this.call<void>("POST", "/log", { msg, fields }, 3_000); }
  /** The local daemon's Settings (capture rules, category folders). */
  settings(timeoutMs = 5_000) { return this.call<CaptureRules>("GET", "/settings", undefined, timeoutMs); }
  probe(request: DownloadRequest) { return this.call<Probe>("POST", "/probe", { request }, 15_000); }
  /** Native folder chooser on the local daemon; "" when cancelled. Waits for the user. */
  pickFolder(initial: string) {
    return this.call<{ path: string }>("POST", "/pickfolder", { initial }, 10 * 60_000).then((r) => r.path);
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
    throw new Error(t("err.notRunning", { err: (e as Error).message }));
  }
  if (reply && reply.ok === false) throw new Error(t("err.startFailed", { err: reply.error ?? "?" }));
  for (let i = 0; i < 40; i++) {
    await sleep(250);
    try {
      await c.info();
      return c;
    } catch {
      /* still starting */
    }
  }
  throw new Error(t("err.noAnswer", { url: LOCAL_URL }));
}

/** Client for a destination; starts the local ODM when needed. */
export async function clientFor(target: Target, s: Settings): Promise<OdmClient> {
  const remote = remoteOf(s);
  if (target === "remote" && remote) return new OdmClient(remote.url, remote.token);
  return ensureLocal();
}

/** Human name of a destination for status messages. */
export function targetName(target: Target, s: Settings): string {
  const remote = remoteOf(s);
  return target === "remote" && remote ? t("bg.remote", { host: remote.label }) : t("bg.local");
}

export async function loadSettings(): Promise<Settings> {
  const s = (await ext.storage.local.get({ ...DEFAULTS })) as Settings;
  s.recentDirs = { local: s.recentDirs?.local ?? [], remote: s.recentDirs?.remote ?? [] };
  setLang(s.language);
  return s;
}

/** Remembers a folder at the top of the recent list for that destination. */
export async function rememberDir(target: Target, dir: string) {
  if (!dir) return;
  const s = await loadSettings();
  const list = [dir, ...s.recentDirs[target].filter((d) => d !== dir)].slice(0, 8);
  await ext.storage.local.set({ recentDirs: { ...s.recentDirs, [target]: list } });
}

/** Temporary hand-off data between the background script and the dialog window. */
export const scratch = ext.storage.session ?? ext.storage.local;

export interface Pending {
  req: DownloadRequest;
  hints: Hints;
  windowId?: number;
}

/** Best guess at a file name from a URL. */
export function nameFromUrl(url: string): string {
  try {
    const last = new URL(url).pathname.split("/").filter(Boolean).pop();
    return last ? decodeURIComponent(last) : new URL(url).host;
  } catch {
    return url;
  }
}

export { human };

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
