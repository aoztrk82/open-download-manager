// UI strings of the extension. The language follows the "language" setting
// ("auto" = the browser's UI language). scripts/i18n-check.mjs checks that every
// language has every key and that every key used in src/ and static/ exists.
// Values may contain simple HTML; interpolated data must be escaped by callers.

export const STRINGS = {
  en: {
    "brand.tagline": "Open Download Manager",
    "pop.capture": "Capture", "pop.running": "Running", "pop.notRunning": "Not running",
    "pop.start": "Start ODM", "pop.starting": "Starting ODM…", "pop.open": "Open ODM", "pop.settings": "Settings",
    "pop.empty": "No downloads yet.", "pop.noEvent": "No downloads captured yet.",
    "pop.notRunningHint": "ODM starts by itself when a download is captured.",
    "status.active": "Downloading", "status.queued": "Queued", "status.paused": "Paused",
    "status.completed": "Completed", "status.failed": "Failed",
    "ver.mismatch": "This extension is {ext}, ODM {odm} bundles {bundled}. Update it from ODM: Integrations > Browser Extensions.",
    "dlg.title": "New download", "dlg.where": "Download to", "dlg.local": "This computer", "dlg.localSub": "ODM on this PC",
    "dlg.remote": "Server", "dlg.folder": "Folder", "dlg.browse": "Browse…", "dlg.defaultDir": "Empty = default folder: {dir}",
    "dlg.serverDir": "Must be inside the server's folder: {dir}", "dlg.starting": "Starting ODM…",
    "dlg.dontAsk": "Don't ask again, always use this destination (change it in Settings)",
    "dlg.browser": "Let the browser download it", "dlg.later": "Later", "dlg.download": "Download",
    "dlg.closeNote": "Closing this window cancels the download.", "dlg.sending": "Sending…",
    "dlg.unknownSize": "Unknown size", "dlg.resumable": "Resumable", "dlg.notResumable": "Not resumable",
    "dlg.gone": "Nothing to download here any more.", "dlg.name": "File name",
    "opt.title": "ODM settings", "opt.general": "General", "opt.language": "Language", "opt.langAuto": "Browser language",
    "opt.capture": "Capture browser downloads", "opt.ask": "Ask where to save before every download",
    "opt.askHelp": "Shows a dialog with file name, size, destination and folder before the download starts.",
    "opt.defaultTarget": "When not asking, send downloads to", "opt.minSize": "Only capture files larger than (MB, 0 = all)",
    "opt.local": "This computer", "opt.localHelp": "Nothing to set up: downloads go to the ODM on this computer. If it is closed, the extension starts it.",
    "opt.checking": "Checking…", "opt.startTest": "Start / test local ODM",
    "opt.localOk": "Running (ODM {v}, downloads go to {dir}).",
    "opt.localOff": "Not running right now. It is started automatically when a download is captured.",
    "opt.server": "Server (optional)", "opt.serverHelp": "Add an ODM server to choose between this computer and the server for each download.",
    "opt.serverUrl": "Server URL", "opt.token": "Server token",
    "opt.tokenHelp": "The token the server was started with (<code>odm serve --listen … --token …</code>).",
    "opt.removeServer": "Remove server", "opt.save": "Save", "opt.saved": "Saved.",
    "opt.savedLocal": "Saved. Downloads go to this computer.",
    "opt.savedServer": "Saved. Server {host} answered (ODM {v}).",
    "opt.savedServerFail": "Saved, but the server did not answer: {err}",
    "opt.version": "Extension {v}",
    "bg.sent": "Sent to {where}: {url}", "bg.local": "this computer", "bg.remote": "server {host}", "bg.later": "Added paused to {where}: {url}",
    "bg.giveBack": "{err} The download was given back to the browser.", "bg.cancelled": "Download cancelled: {url}",
    "bg.browser": "Left to the browser: {url}", "bg.kept": "Browser kept its copy ({why})",
    "bg.error": "Capture error: {err}", "bg.menu": "Download with ODM",
    "opt.videoButton": "Show a download button on videos",
    "opt.videoButtonHelp": "Hover a playing video to get a \"Download with ODM\" button that lists its qualities.",
    "vid.thisVideo": "This video", "vid.pageVideos": "Other videos on this page", "vid.playing": "playing",
    "vid.stream": "streaming playlist, not supported yet", "vid.none": "No downloadable source found for this video.",
    "vid.mse": "This video is streamed in pieces (MSE); ODM cannot download it yet.",
    "vid.sent": "Sent to ODM", "vid.failed": "Could not send: {err}",
    "vid.ytdlp": "With yt-dlp", "vid.best": "Best quality", "vid.audio": "Audio only",
    "err.notRunning": "ODM is not running and could not be started automatically. Start odm.exe once (it registers the browser integration), then try again. ({err})",
    "err.startFailed": "ODM could not be started: {err}", "err.noAnswer": "ODM was started but is not answering on {url}",
  },
  tr: {
    "brand.tagline": "Açık İndirme Yöneticisi",
    "pop.capture": "Yakala", "pop.running": "Çalışıyor", "pop.notRunning": "Çalışmıyor",
    "pop.start": "ODM'yi Başlat", "pop.starting": "ODM başlatılıyor…", "pop.open": "ODM'yi Aç", "pop.settings": "Ayarlar",
    "pop.empty": "Henüz indirme yok.", "pop.noEvent": "Henüz yakalanan indirme yok.",
    "pop.notRunningHint": "Bir indirme yakalandığında ODM kendiliğinden başlar.",
    "status.active": "İndiriliyor", "status.queued": "Sırada", "status.paused": "Duraklatıldı",
    "status.completed": "Tamamlandı", "status.failed": "Başarısız",
    "ver.mismatch": "Bu eklenti {ext}, ODM {odm} içindeki sürüm {bundled}. ODM'den güncelleyin: Entegrasyonlar > Tarayıcı Eklentileri.",
    "dlg.title": "Yeni indirme", "dlg.where": "Nereye indirilsin", "dlg.local": "Bu bilgisayar", "dlg.localSub": "Bu bilgisayardaki ODM",
    "dlg.remote": "Sunucu", "dlg.folder": "Klasör", "dlg.browse": "Gözat…", "dlg.defaultDir": "Boş = varsayılan klasör: {dir}",
    "dlg.serverDir": "Sunucunun klasörü içinde olmalı: {dir}", "dlg.starting": "ODM başlatılıyor…",
    "dlg.dontAsk": "Bir daha sorma, hep bu hedefi kullan (Ayarlar'dan değiştirilebilir)",
    "dlg.browser": "Tarayıcı indirsin", "dlg.later": "Sonra", "dlg.download": "İndir",
    "dlg.closeNote": "Bu pencereyi kapatmak indirmeyi iptal eder.", "dlg.sending": "Gönderiliyor…",
    "dlg.unknownSize": "Boyut bilinmiyor", "dlg.resumable": "Devam ettirilebilir", "dlg.notResumable": "Devam ettirilemez",
    "dlg.gone": "Burada indirilecek bir şey kalmadı.", "dlg.name": "Dosya adı",
    "opt.title": "ODM ayarları", "opt.general": "Genel", "opt.language": "Dil", "opt.langAuto": "Tarayıcının dili",
    "opt.capture": "Tarayıcı indirmelerini yakala", "opt.ask": "Her indirmeden önce nereye kaydedileceğini sor",
    "opt.askHelp": "İndirme başlamadan önce dosya adı, boyut, hedef ve klasörü gösteren bir pencere açar.",
    "opt.defaultTarget": "Sormadığında indirmeleri gönder", "opt.minSize": "Yalnızca şundan büyük dosyaları yakala (MB, 0 = hepsi)",
    "opt.local": "Bu bilgisayar", "opt.localHelp": "Ayar gerekmez: indirmeler bu bilgisayardaki ODM'ye gider. Kapalıysa eklenti onu başlatır.",
    "opt.checking": "Kontrol ediliyor…", "opt.startTest": "Yerel ODM'yi başlat / dene",
    "opt.localOk": "Çalışıyor (ODM {v}, indirmeler {dir} klasörüne).",
    "opt.localOff": "Şu an çalışmıyor. Bir indirme yakalandığında otomatik başlar.",
    "opt.server": "Sunucu (isteğe bağlı)", "opt.serverHelp": "Bir ODM sunucusu eklerseniz her indirmede bu bilgisayar ile sunucu arasında seçim yaparsınız.",
    "opt.serverUrl": "Sunucu adresi", "opt.token": "Sunucu anahtarı",
    "opt.tokenHelp": "Sunucunun başlatıldığı anahtar (<code>odm serve --listen … --token …</code>).",
    "opt.removeServer": "Sunucuyu kaldır", "opt.save": "Kaydet", "opt.saved": "Kaydedildi.",
    "opt.savedLocal": "Kaydedildi. İndirmeler bu bilgisayara gider.",
    "opt.savedServer": "Kaydedildi. {host} sunucusu yanıt verdi (ODM {v}).",
    "opt.savedServerFail": "Kaydedildi ama sunucu yanıt vermedi: {err}",
    "opt.version": "Eklenti {v}",
    "bg.sent": "{where} gönderildi: {url}", "bg.local": "bu bilgisayara", "bg.remote": "{host} sunucusuna", "bg.later": "{where} duraklatılmış eklendi: {url}",
    "bg.giveBack": "{err} İndirme tarayıcıya geri verildi.", "bg.cancelled": "İndirme iptal edildi: {url}",
    "bg.browser": "Tarayıcıya bırakıldı: {url}", "bg.kept": "Tarayıcı kendi kopyasını tuttu ({why})",
    "bg.error": "Yakalama hatası: {err}", "bg.menu": "ODM ile indir",
    "opt.videoButton": "Videolarda indirme butonu göster",
    "opt.videoButtonHelp": "Oynayan bir videonun üstüne gelince kalitelerini listeleyen \"ODM ile indir\" butonu çıkar.",
    "vid.thisVideo": "Bu video", "vid.pageVideos": "Sayfadaki diğer videolar", "vid.playing": "oynatılıyor",
    "vid.stream": "akış listesi, henüz desteklenmiyor", "vid.none": "Bu video için indirilebilir bir kaynak bulunamadı.",
    "vid.mse": "Bu video parça parça akışla (MSE) oynatılıyor; ODM henüz indiremiyor.",
    "vid.sent": "ODM'ye gönderildi", "vid.failed": "Gönderilemedi: {err}",
    "vid.ytdlp": "yt-dlp ile", "vid.best": "En iyi kalite", "vid.audio": "Sadece ses",
    "err.notRunning": "ODM çalışmıyor ve otomatik başlatılamadı. odm.exe'yi bir kez çalıştırın (tarayıcı entegrasyonunu kaydeder), sonra tekrar deneyin. ({err})",
    "err.startFailed": "ODM başlatılamadı: {err}", "err.noAnswer": "ODM başlatıldı ama {url} adresinde yanıt vermiyor",
  },
};

export type Lang = keyof typeof STRINGS;
type Key = keyof (typeof STRINGS)["en"];

let lang: Lang = "en";

/**
 * Picks the UI language from the setting ("auto" = browser language). The
 * content script passes markDocument=false: that document is the web page's.
 */
export function setLang(setting: string | undefined, markDocument = true): Lang {
  const want = !setting || setting === "auto" ? (navigator.language || "en").slice(0, 2).toLowerCase() : setting;
  lang = (want in STRINGS ? want : "en") as Lang;
  if (markDocument && typeof document !== "undefined") document.documentElement.lang = lang;
  return lang;
}
export const currentLang = () => lang;

export function t(key: Key, vars: Record<string, string | number> = {}): string {
  const s: string = (STRINGS[lang] as Record<string, string>)[key] ?? STRINGS.en[key] ?? key;
  return s.replace(/\{(\w+)\}/g, (_, n: string) => String(vars[n] ?? ""));
}

/**
 * Puts a translated string into an element. Strings may carry <b>/<code>; they
 * come from this file, never from page data, and are parsed inertly (no
 * innerHTML, so scripts and handlers in them could not run anyway).
 */
export function setText(el: Element, key: Key, vars: Record<string, string | number> = {}) {
  const s = t(key, vars);
  if (!s.includes("<")) {
    el.textContent = s;
    return;
  }
  const doc = new DOMParser().parseFromString(`<body>${s}</body>`, "text/html");
  el.replaceChildren(...Array.from(doc.body.childNodes));
}

/** Builds an element: h("span", "badge ok", child, "text"). */
export function h(tag: string, cls = "", ...children: (Node | string)[]): HTMLElement {
  const el = document.createElement(tag);
  if (cls) el.className = cls;
  el.append(...children);
  return el;
}

/** Fills data-i18n, data-i18n-title and data-i18n-ph attributes. */
export function applyI18n(root: ParentNode = document) {
  root.querySelectorAll<HTMLElement>("[data-i18n]").forEach((el) => setText(el, el.dataset.i18n as Key));
  root.querySelectorAll<HTMLElement>("[data-i18n-title]").forEach((el) => (el.title = t(el.dataset.i18nTitle as Key)));
  root
    .querySelectorAll<HTMLInputElement>("[data-i18n-ph]")
    .forEach((el) => (el.placeholder = t(el.dataset.i18nPh as Key)));
}

export const esc = (s: unknown) =>
  String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
