// Firefox exposes a promise-based `browser` namespace; its `chrome` namespace is
// callback-based there, so `await chrome.storage.local.get()` can yield undefined.
// Chrome has no `browser`, and its `chrome.*` calls return promises in MV3.
export const ext: typeof chrome =
  (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome;
