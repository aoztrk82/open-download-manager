// The download dialog: shown before a captured download starts. The user can
// rename the file, pick this computer or the server, choose a folder, and start
// now or later. Closing the window cancels (the background script reports it).
import {
  clientFor,
  ext,
  extVersion,
  human,
  loadSettings,
  nameFromUrl,
  rememberDir,
  remoteOf,
  report,
  scratch,
  targetName,
  type Info,
  type OdmClient,
  type Pending,
  type Probe,
  type Settings,
  type Target,
} from "./api";
import { applyI18n, h, t } from "./i18n";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const id = new URLSearchParams(location.search).get("id") ?? "";
const key = `pending:${id}`;

interface TargetState {
  client?: OdmClient;
  info?: Info;
  probe?: Probe | null;
  error?: string;
  loading?: Promise<void>;
}

async function main() {
  const s: Settings = await loadSettings();
  applyI18n();
  $("ext-ver").textContent = "v" + extVersion();
  const rec = ((await scratch.get(key)) as Record<string, Pending>)[key];
  if (!rec) {
    document.body.replaceChildren(h("p", "muted", t("dlg.gone")));
    return;
  }
  const { req, hints } = rec;
  const remote = remoteOf(s);
  const status = $("status");
  const nameInput = $<HTMLInputElement>("name");
  const dirInput = $<HTMLInputElement>("dir");
  const buttons = ["btn-download", "btn-later", "btn-browser", "browse"].map((b) => $<HTMLButtonElement>(b));

  // ---- file facts: browser hints first, probe fills the gaps ----
  const suggested = hints.filename || nameFromUrl(req.url);
  nameInput.value = suggested;
  let userRenamed = false;
  nameInput.addEventListener("input", () => (userRenamed = true));
  $("host").textContent = new URL(req.url).host;
  $("host").title = req.url;
  const showFacts = (p?: Probe | null) => {
    const size = hints.size || (p && p.size > 0 ? p.size : 0);
    $("size").textContent = size ? human(size) : t("dlg.unknownSize");
    const resumable = hints.resumable ?? p?.acceptRanges;
    $("resume").replaceChildren(
      ...(resumable === undefined
        ? []
        : [h("span", "badge" + (resumable ? " ok" : ""), resumable ? t("dlg.resumable") : t("dlg.notResumable"))]),
    );
    const fileExt = (nameInput.value.match(/\.([a-z0-9]{1,5})$/i)?.[1] ?? "").toUpperCase();
    $("ext-label").textContent = fileExt;
  };
  nameInput.addEventListener("input", () => showFacts(state.local.probe ?? state.remote.probe));

  // ---- destinations ----
  const state: Record<Target, TargetState> = { local: {}, remote: {} };
  if (remote) $("remote-label").textContent = remote.label;
  else {
    $("choice-remote").hidden = true;
    $("choices").classList.add("single");
  }
  let target: Target = s.defaultTarget === "remote" && remote ? "remote" : "local";
  document.querySelector<HTMLInputElement>(`input[name=target][value=${target}]`)!.checked = true;

  function load(tg: Target): Promise<void> {
    const st = state[tg];
    st.loading ??= (async () => {
      try {
        if (tg === "local") status.textContent = t("dlg.starting");
        st.client = await clientFor(tg, s);
        st.info = await st.client.info();
        if (!hints.size || hints.resumable === undefined || !hints.filename) {
          st.probe = await st.client.probe(req).catch(() => null);
          if (st.probe?.filename && !hints.filename && !userRenamed) nameInput.value = st.probe.filename;
        }
        if (status.textContent === t("dlg.starting")) status.textContent = "";
      } catch (e) {
        st.error = (e as Error).message;
      }
    })();
    return st.loading;
  }

  function showTarget() {
    const st = state[target];
    const recent = s.recentDirs[target];
    $("recent").replaceChildren(
      ...recent.map((d) => {
        const o = document.createElement("option");
        o.value = d; // value, not innerHTML: folder names are user data
        return o;
      }),
    );
    dirInput.value = recent[0] ?? "";
    $<HTMLButtonElement>("browse").hidden = !(target === "local" && st.info?.canPickFolder);
    const hint = $("dir-hint");
    hint.className = "hint";
    if (st.error) {
      hint.textContent = st.error;
      hint.className = "hint bad";
    } else if (st.info) {
      hint.textContent = st.info.anyFolder === false
        ? t("dlg.serverDir", { dir: st.info.defaultDir })
        : t("dlg.defaultDir", { dir: st.info.defaultDir });
      dirInput.placeholder = st.info.defaultDir;
    } else hint.textContent = "";
    showFacts(st.probe);
  }

  async function switchTo(tg: Target) {
    target = tg;
    showTarget();
    await load(tg);
    if (target === tg) showTarget();
  }
  document.querySelectorAll<HTMLInputElement>("input[name=target]").forEach((r) =>
    r.addEventListener("change", () => r.checked && switchTo(r.value as Target)),
  );

  // ---- actions ----
  const busy = (on: boolean) => buttons.forEach((b) => (b.disabled = on));

  $("browse").addEventListener("click", async () => {
    const st = state[target];
    if (!st.client) return;
    busy(true);
    try {
      const path = await st.client.pickFolder(dirInput.value.trim() || st.info?.defaultDir || "");
      if (path) dirInput.value = path;
    } catch (e) {
      status.textContent = (e as Error).message;
      status.className = "bad";
    } finally {
      busy(false);
    }
  });

  async function send(kind: "now" | "later" | "browser") {
    busy(true);
    status.className = "";
    status.textContent = t("dlg.sending");
    const filename = nameInput.value.trim();
    try {
      if (kind === "browser") {
        const r = (await ext.runtime.sendMessage({
          type: "browser-download",
          url: req.url,
          filename: userRenamed ? filename : undefined,
        })) as { ok: boolean; error?: string } | undefined;
        if (r && !r.ok) throw new Error(r.error ?? "browser download failed");
      } else {
        await load(target);
        const st = state[target];
        if (!st.client) throw new Error(st.error ?? "ODM is not available");
        const dir = dirInput.value.trim();
        // Only override the server's own name when it was edited or came from
        // the browser; otherwise ODM picks it from the response, as usual.
        await st.client.add(req, {
          filename: userRenamed || hints.filename ? filename : undefined,
          dir: dir || undefined,
          paused: kind === "later",
        });
        await rememberDir(target, dir);
        if ($<HTMLInputElement>("dont-ask").checked) {
          await ext.storage.local.set({ askBeforeDownload: false, defaultTarget: target });
        }
      }
      await scratch.remove(key); // before closing, so the window-closed handler sees it as a decision
      const where = targetName(target, s);
      await report(
        true,
        kind === "browser"
          ? t("bg.browser", { url: req.url })
          : t(kind === "later" ? "bg.later" : "bg.sent", { where, url: req.url }),
      );
      window.close();
    } catch (e) {
      status.className = "bad";
      status.textContent = (e as Error).message;
      busy(false);
    }
  }

  $("btn-download").addEventListener("click", () => send("now"));
  $("btn-later").addEventListener("click", () => send("later"));
  $("btn-browser").addEventListener("click", () => send("browser"));
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") window.close();
    else if (e.key === "Enter" && !(e.target as HTMLElement).matches("button")) {
      e.preventDefault();
      if (!$<HTMLButtonElement>("btn-download").disabled) send("now");
    }
  });

  showFacts();
  $("btn-download").focus();
  await switchTo(target);
}

main();
