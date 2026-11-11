import { DEFAULTS, OdmClient, ensureLocal, ext, extVersion, loadSettings, localClient, remoteOf, type Settings, type Target } from "./api";
import { applyI18n, h, setLang, t } from "./i18n";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const input = (id: string) => $<HTMLInputElement>(id);

function say(el: HTMLElement, ok: boolean | null, text: string) {
  el.textContent = text;
  el.className = "status " + (ok === null ? "muted" : ok ? "ok" : "bad");
}

async function checkLocal(start: boolean) {
  const el = $("local-status");
  const badge = $("local-badge");
  say(el, null, start ? t("pop.starting") : t("opt.checking"));
  try {
    const info = await (start ? ensureLocal() : Promise.resolve(localClient())).then((c) => c.info());
    badge.className = "badge ok";
    badge.replaceChildren(h("i", "dot"), " " + t("pop.running"));
    say(el, true, t("opt.localOk", { v: info.version, dir: info.defaultDir }));
  } catch (e) {
    badge.className = "badge";
    badge.replaceChildren(h("i", "dot"), " " + t("pop.notRunning"));
    say(el, start ? false : null, start ? (e as Error).message : t("opt.localOff"));
  }
}

function syncRemoteOption() {
  const url = input("serverUrl").value.trim();
  const remote = remoteOf({ ...DEFAULTS, serverUrl: url });
  const opt = $<HTMLOptionElement>("opt-remote");
  opt.textContent = remote ? `${t("dlg.remote")} · ${remote.label}` : t("dlg.remote");
  opt.disabled = !remote;
  if (!remote) $<HTMLSelectElement>("defaultTarget").value = "local";
}

function render() {
  applyI18n();
  $("ver").textContent = t("opt.version", { v: extVersion() });
  document.title = t("opt.title");
  syncRemoteOption();
}

async function init() {
  const s = await loadSettings();
  $<HTMLSelectElement>("language").value = s.language;
  input("enabled").checked = s.enabled;
  input("askBeforeDownload").checked = s.askBeforeDownload;
  $<HTMLSelectElement>("defaultTarget").value = s.defaultTarget;
  input("minSizeMB").value = String(s.minSizeMB);
  input("serverUrl").value = s.serverUrl;
  input("token").value = s.token;
  render();
  $<HTMLSelectElement>("defaultTarget").value = s.defaultTarget; // after the option labels exist
  syncRemoteOption();
  checkLocal(false);
}

function read(): Partial<Settings> {
  return {
    language: $<HTMLSelectElement>("language").value,
    enabled: input("enabled").checked,
    askBeforeDownload: input("askBeforeDownload").checked,
    defaultTarget: $<HTMLSelectElement>("defaultTarget").value as Target,
    minSizeMB: Math.max(0, Number(input("minSizeMB").value) || 0),
    serverUrl: input("serverUrl").value.trim(),
    token: input("token").value.trim(),
  };
}

$("language").addEventListener("change", () => {
  // Preview right away; it is stored with the other settings on Save.
  setLang($<HTMLSelectElement>("language").value);
  render();
  checkLocal(false);
});
input("serverUrl").addEventListener("input", syncRemoteOption);
$("start-local").addEventListener("click", () => checkLocal(true));
$("clear-server").addEventListener("click", () => {
  input("serverUrl").value = "";
  input("token").value = "";
  syncRemoteOption();
});

$("save").addEventListener("click", async () => {
  const patch = read();
  await ext.storage.local.set(patch);
  const s = await loadSettings();
  render();
  const status = $("status");
  const remote = remoteOf(s);
  if (!remote) {
    say(status, true, t("opt.savedLocal"));
    return;
  }
  say(status, null, t("opt.saved"));
  try {
    const info = await new OdmClient(remote.url, remote.token).info();
    say(status, true, t("opt.savedServer", { host: remote.label, v: info.version }));
  } catch (err) {
    say(status, false, t("opt.savedServerFail", { err: (err as Error).message }));
  }
});

init();
