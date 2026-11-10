import { ext, DEFAULTS, OdmClient, loadSettings, type Settings } from "./api";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

async function init() {
  const s = await loadSettings();
  $<HTMLInputElement>("serverUrl").value = s.serverUrl;
  $<HTMLInputElement>("token").value = s.token;
  $<HTMLInputElement>("minSizeMB").value = String(s.minSizeMB);
  $<HTMLInputElement>("enabled").checked = s.enabled;
}

function read(): Settings {
  return {
    enabled: $<HTMLInputElement>("enabled").checked,
    serverUrl: $<HTMLInputElement>("serverUrl").value.trim() || DEFAULTS.serverUrl,
    token: $<HTMLInputElement>("token").value.trim(),
    minSizeMB: Number($<HTMLInputElement>("minSizeMB").value) || 0,
  };
}

$("form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const s = read();
  await ext.storage.local.set({ ...s });
  const status = $("status");
  try {
    const info = await new OdmClient(s.serverUrl, s.token).info();
    status.textContent = `Saved. Connected to ODM ${info.version} (downloads go to ${info.defaultDir}).`;
    status.className = "ok";
  } catch (err) {
    status.textContent = `Saved, but cannot connect: ${(err as Error).message}`;
    status.className = "bad";
  }
});

init();
