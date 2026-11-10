import { DEFAULTS, OdmClient, ensureLocal, ext, loadSettings, localClient, remoteOf, type Settings } from "./api";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

function say(el: HTMLElement, ok: boolean, text: string) {
  el.textContent = text;
  el.className = "status " + (ok ? "ok" : "bad");
}

async function checkLocal(start: boolean) {
  const el = $("local-status");
  try {
    const info = await (start ? ensureLocal() : Promise.resolve(localClient())).then((c) => c.info());
    say(el, true, `Running (ODM ${info.version}, downloads go to ${info.defaultDir}).`);
  } catch (e) {
    say(
      el,
      false,
      start
        ? (e as Error).message
        : "Not running right now. It is started automatically when a download is captured.",
    );
  }
}

async function init() {
  const s = await loadSettings();
  $<HTMLInputElement>("serverUrl").value = s.serverUrl;
  $<HTMLInputElement>("token").value = s.token;
  $<HTMLInputElement>("minSizeMB").value = String(s.minSizeMB);
  $<HTMLInputElement>("enabled").checked = s.enabled;
  checkLocal(false);
}

function read(): Settings {
  return {
    enabled: $<HTMLInputElement>("enabled").checked,
    serverUrl: $<HTMLInputElement>("serverUrl").value.trim() || DEFAULTS.serverUrl,
    token: $<HTMLInputElement>("token").value.trim(),
    minSizeMB: Number($<HTMLInputElement>("minSizeMB").value) || 0,
  };
}

$("start-local").addEventListener("click", () => {
  say($("local-status"), true, "Starting...");
  checkLocal(true);
});

$("clear-server").addEventListener("click", () => {
  $<HTMLInputElement>("serverUrl").value = "";
  $<HTMLInputElement>("token").value = "";
});

$("form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const s = read();
  await ext.storage.local.set({ ...s });
  const status = $("status");
  const remote = remoteOf(s);
  if (!remote) {
    say(status, true, "Saved. Downloads will go to this computer.");
    return;
  }
  try {
    const info = await new OdmClient(remote.url, remote.token).info();
    say(status, true, `Saved. Server ${remote.label} answered (ODM ${info.version}). Captured downloads will ask where to go.`);
  } catch (err) {
    say(status, false, `Saved, but the server did not answer: ${(err as Error).message}`);
  }
});

init();
