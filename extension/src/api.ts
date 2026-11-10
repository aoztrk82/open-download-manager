// Minimal client for the ODM HTTP API (same protocol the Go CLI and Web UI use).

// Firefox exposes a promise-based `browser` namespace; its `chrome` namespace is
// callback-based there, so `await chrome.storage.local.get()` can yield undefined.
// Chrome has no `browser`, and its `chrome.*` calls return promises in MV3.
export const ext: typeof chrome =
  (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome;

export interface Settings {
  enabled: boolean;
  serverUrl: string;
  token: string;
  minSizeMB: number;
}

export const DEFAULTS: Settings = {
  enabled: true,
  serverUrl: "http://127.0.0.1:6800",
  token: "",
  minSizeMB: 0,
};

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

  constructor(base: string, private token: string) {
    this.base = base.replace(/\/+$/, "");
  }

  private async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const r = await fetch(this.base + "/v1" + path, {
      method,
      headers: { Authorization: `Bearer ${this.token}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!r.ok) {
      const e = await r.json().catch(() => ({}));
      throw new Error((e as { error?: string }).error ?? `${r.status} ${r.statusText}`);
    }
    return r.status === 204 ? (undefined as T) : ((await r.json()) as T);
  }

  info() { return this.call<{ version: string; defaultDir: string }>("GET", "/info"); }
  list() { return this.call<Download[]>("GET", "/downloads"); }
  add(request: DownloadRequest, filename?: string) {
    return this.call<Download>("POST", "/downloads", { request, filename });
  }
  pause(id: string) { return this.call<void>("POST", `/downloads/${id}/pause`); }
  resume(id: string) { return this.call<void>("POST", `/downloads/${id}/resume`); }
  remove(id: string) { return this.call<void>("DELETE", `/downloads/${id}`); }
}

export async function loadSettings(): Promise<Settings> {
  const s = await ext.storage.local.get({ ...DEFAULTS });
  return s as Settings;
}

export async function clientFromSettings(): Promise<OdmClient> {
  const s = await loadSettings();
  return new OdmClient(s.serverUrl, s.token);
}
