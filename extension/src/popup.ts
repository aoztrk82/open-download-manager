import { clientFromSettings, loadSettings } from "./api";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

function human(n: number) {
  if (n < 1024) return `${n} B`;
  const u = "KMGTP";
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < u.length - 1);
  return `${n.toFixed(1)} ${u[i]}iB`;
}

async function refresh() {
  const list = $("list");
  try {
    const odm = await clientFromSettings();
    const items = (await odm.list()).reverse().slice(0, 15);
    list.replaceChildren(
      ...items.map((d) => {
        const li = document.createElement("li");
        const pct = d.size > 0 ? Math.floor((d.downloaded * 100) / d.size) : 0;
        const name = document.createElement("div");
        name.className = "name";
        name.textContent = d.filename || d.request.url; // textContent: never innerHTML for remote data
        const meta = document.createElement("div");
        meta.className = "meta";
        meta.textContent = `${d.status} - ${human(d.downloaded)}${d.size > 0 ? ` / ${human(d.size)} (${pct}%)` : ""}`;
        li.append(name, meta);
        return li;
      }),
    );
    if (!items.length) list.textContent = "No downloads yet.";
    $("err").textContent = "";
  } catch (e) {
    list.replaceChildren();
    $("err").textContent = `Cannot reach ODM: ${(e as Error).message}. Check settings.`;
  }
}

async function init() {
  const s = await loadSettings();
  const toggle = $<HTMLInputElement>("enabled");
  toggle.checked = s.enabled;
  toggle.addEventListener("change", () => chrome.storage.local.set({ enabled: toggle.checked }));
  $("open").addEventListener("click", async () => {
    const cur = await loadSettings();
    chrome.tabs.create({ url: cur.serverUrl });
  });
  $("settings").addEventListener("click", () => chrome.runtime.openOptionsPage());
  refresh();
  setInterval(refresh, 1500);
}

init();
