const AUTH_STORAGE_KEY = "autoget_auth";
const REFRESH_LOCK = "autoget_auth_refresh";
const AUTH_CHANNEL_NAME = "autoget_auth_channel";
// Cross-tab refresh lock kept in localStorage for browsers/site origins where
// the Web Locks API is unavailable (it is restricted to secure contexts). See
// acquireTabLock.
const TAB_LOCK_KEY = "autoget_auth_refresh_lock";
// A tab that holds the lock is assumed to have crashed once it is older than
// this, so a crashed holder cannot deadlock every other tab forever. It must
// comfortably exceed the time a single refresh takes.
const TAB_LOCK_TTL_MS = 10_000;
// Upper bound for the refresh request. It runs while holding the cross-tab
// lock, so it must stay below TAB_LOCK_TTL_MS or the fallback lock could expire
// under a live holder.
const REFRESH_FETCH_TIMEOUT_MS = 8_000;
// How long a contender waits for the current holder before refreshing itself.
const TAB_LOCK_WAIT_MS = 10_000;
// Settle time before a contender confirms it won the lock; concurrent writers
// surface their write within this window.
const TAB_LOCK_SETTLE_MS = 50;
// A refresh token is single-use, so when two tabs exchange the same token at
// once one of them is rejected even though the session is healthy. This bounds
// how long the loser waits for the winner to publish its tokens before it
// treats the session as gone. The timeout covers network latency plus token
// exchange at the OAuth provider, which may involve remote JWKS validation.
const SESSION_HANDOFF_TIMEOUT_MS = 2500;

interface StoredTokens {
  access_token: string;
  refresh_token: string;
  id_token: string;
  expires_in: number;
  token_type: string;
}

function getTokens(): StoredTokens | null {
  try {
    const raw = localStorage.getItem(AUTH_STORAGE_KEY);
    return raw ? (JSON.parse(raw) as StoredTokens) : null;
  } catch {
    return null;
  }
}

// Identifies this page so it can ignore its own broadcasts: separate
// BroadcastChannel instances in one page do receive each other's messages.
const TAB_ID = randomId();

function postAuthMessage(message: { type: string; tokens?: StoredTokens }): void {
  try {
    if (typeof BroadcastChannel !== "undefined") {
      const channel = new BroadcastChannel(AUTH_CHANNEL_NAME);
      channel.postMessage({ ...message, sender: TAB_ID });
      channel.close();
    }
  } catch {
    /* BroadcastChannel not available or failed */
  }
}

function broadcastSessionUpdate(tokens: StoredTokens): void {
  postAuthMessage({ type: "session_updated", tokens });
}

function setTokens(tokens: StoredTokens): void {
  localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(tokens));
  broadcastSessionUpdate(tokens);
}

// isAuthenticated reports whether a session is stored locally. It is only true
// when auth is enabled and the user has logged in.
export function isAuthenticated(): boolean {
  return getTokens() !== null;
}

export function logout(): void {
  localStorage.removeItem(AUTH_STORAGE_KEY);
  // Tell the other tabs; the storage event covers browsers without
  // BroadcastChannel.
  postAuthMessage({ type: "session_cleared" });
  // Reloading drops back to the login flow via the 401 handling below.
  window.location.href = "/";
}

function refreshSignal(): AbortSignal | undefined {
  if (typeof AbortSignal !== "undefined" && typeof AbortSignal.timeout === "function") {
    return AbortSignal.timeout(REFRESH_FETCH_TIMEOUT_MS);
  }
  if (typeof AbortController === "undefined") {
    return undefined;
  }
  const controller = new AbortController();
  setTimeout(() => controller.abort(), REFRESH_FETCH_TIMEOUT_MS);
  return controller.signal;
}

type RefreshResult = "ok" | "expired" | "unavailable";

// refreshTokens exchanges the stored refresh token for new tokens via the
// backend (which holds the client secret).
async function refreshTokens(): Promise<RefreshResult> {
  const tokens = getTokens();
  if (!tokens?.refresh_token) {
    return "expired";
  }

  let response: Response;
  try {
    response = await fetch("/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: tokens.refresh_token }),
      signal: refreshSignal(),
    });
  } catch {
    // Network failure or timeout: the tokens may still be valid, so keep the
    // session.
    return "unavailable";
  }

  if (response.ok) {
    try {
      setTokens((await response.json()) as StoredTokens);
    } catch {
      // Malformed body: nothing was stored, so the old session is untouched.
      return "unavailable";
    }
    return "ok";
  }
  if (response.status === 401) {
    // The refresh token is gone; the user has to log in again. A 400 means the
    // request itself was malformed, which says nothing about the session.
    return "expired";
  }
  // Provider outage (5xx/429): do not log the user out.
  return "unavailable";
}

interface SessionStamp {
  access: string | undefined;
  refresh: string | undefined;
}

function sessionStamp(): SessionStamp {
  const tokens = getTokens();
  return { access: tokens?.access_token, refresh: tokens?.refresh_token };
}

// sessionRefreshedElsewhere reports whether a different, non-empty session is
// now stored than the one captured in before. The provider rotates the
// single-use refresh token, so a tab that loses a cross-tab refresh race must
// adopt the winner's session instead of forcing the user back through login.
// The winner writes around the time our own rejection arrives, so wait for the
// cross-tab BroadcastChannel or storage event (bounded) before giving up.
function sessionRefreshedElsewhere(before: SessionStamp): Promise<boolean> {
  const changed = (): boolean => {
    const now = sessionStamp();
    return !!now.access && (now.access !== before.access || now.refresh !== before.refresh);
  };

  if (changed()) {
    return Promise.resolve(true);
  }
  if (!before.refresh) {
    // Without a stored refresh token there is nothing another tab could have
    // handed off, so there is no point waiting.
    return Promise.resolve(false);
  }

  return new Promise((resolve) => {
    let settled = false;
    let channel: BroadcastChannel | null = null;
    const finish = (value: boolean): void => {
      if (settled) {
        return;
      }
      settled = true;
      window.removeEventListener("storage", onStorage);
      if (channel) {
        channel.close();
      }
      clearTimeout(timer);
      resolve(value);
    };

    const onStorage = (event: StorageEvent): void => {
      if (event.key === AUTH_STORAGE_KEY && changed()) {
        finish(true);
      }
    };

    try {
      if (typeof BroadcastChannel !== "undefined") {
        channel = new BroadcastChannel(AUTH_CHANNEL_NAME);
        channel.onmessage = (event: MessageEvent) => {
          if (event.data?.type === "session_updated" && changed()) {
            finish(true);
          }
        };
      }
    } catch {
      /* BroadcastChannel not available */
    }

    const timer = setTimeout(() => finish(false), SESSION_HANDOFF_TIMEOUT_MS);
    window.addEventListener("storage", onStorage);
    // The other tab may have written between the check above and here.
    if (changed()) {
      finish(true);
    }
  });
}

let refreshing: Promise<RefreshResult> | null = null;

interface TabLock {
  id: string;
  ts: number;
}

function randomId(): string {
  return Math.random().toString(36).slice(2) + Date.now().toString(36);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function readTabLock(): TabLock | null {
  try {
    const raw = localStorage.getItem(TAB_LOCK_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as TabLock;
    return typeof parsed?.id === "string" && typeof parsed?.ts === "number" ? parsed : null;
  } catch {
    // Malformed value or blocked storage: treat it as unlocked.
    return null;
  }
}

function writeTabLock(lock: TabLock): boolean {
  try {
    localStorage.setItem(TAB_LOCK_KEY, JSON.stringify(lock));
    return true;
  } catch {
    /* storage unavailable (e.g. private mode or quota reached): fall back to the in-page flight immediately */
    return false;
  }
}

function removeTabLock(): void {
  try {
    localStorage.removeItem(TAB_LOCK_KEY);
  } catch {
    /* ignore */
  }
}

// acquireTabLock serialises refreshes across tabs when Web Locks is unavailable.
// Every contender publishes its own lock, waits for concurrent writes to
// surface, then reads back: only the tab whose id survives is the holder. Others
// back off and retry, so two tabs never exchange the same single-use refresh
// token and one of them is not mistaken for a replay. Returns null when the wait
// budget is exhausted, in which case the caller may still refresh (the caller
// re-checks whether another tab already handed off a session).
async function acquireTabLock(): Promise<(() => void) | null> {
  const me: TabLock = { id: randomId(), ts: Date.now() };
  const deadline = Date.now() + TAB_LOCK_WAIT_MS;

  while (Date.now() < deadline) {
    const current = readTabLock();
    const heldByOther =
      !!current && current.id !== me.id && Date.now() - current.ts < TAB_LOCK_TTL_MS;
    if (!heldByOther) {
      me.ts = Date.now();
      if (!writeTabLock(me)) {
        // Storage is blocked or quota exceeded; do not spin until deadline.
        return null;
      }
      await sleep(TAB_LOCK_SETTLE_MS);
      if (readTabLock()?.id === me.id) {
        return () => {
          if (readTabLock()?.id === me.id) {
            removeTabLock();
          }
        };
      }
    }
    await sleep(TAB_LOCK_SETTLE_MS);
  }
  return null;
}

// refreshOnce deduplicates refreshes across every tab. Rotation invalidates the
// previous refresh token, so two tabs refreshing in parallel would make the
// second attempt look like a replay attack and log the user out.
function refreshOnce(): Promise<RefreshResult> {
  const used = getTokens()?.refresh_token;

  const run = async (): Promise<RefreshResult> => {
    // Another tab may have refreshed while we waited for the lock.
    const current = getTokens()?.refresh_token;
    if (used && current && current !== used) {
      return "ok";
    }
    return refreshTokens();
  };

  const locks = (navigator as Navigator & { locks?: LockManager }).locks;
  if (locks) {
    return locks.request(REFRESH_LOCK, run);
  }

  // No Web Locks (insecure context): deduplicate within this page and take the
  // localStorage lock so every other tab queues behind us.
  refreshing ??= (async () => {
    const release = await acquireTabLock();
    try {
      return await run();
    } finally {
      release?.();
    }
  })().finally(() => {
    refreshing = null;
  });
  return refreshing;
}

function redirectToLogin(): void {
  const current = new URL(window.location.href);
  window.location.href = `/auth/login?redirect=${encodeURIComponent(current.pathname + current.search)}`;
}

function redirectToDenied(): void {
  window.location.href = "/auth/denied";
}

// never returns an unresolved promise so callers neither retry nor trigger error
// toasts while the browser is navigating away to a login or denied page.
function never(): Promise<Response> {
  return new Promise(() => {});
}

const sharedNever = never();

// deferLoginRedirect completes the login redirect once the user looks at this
// tab. Only one visibilitychange listener is ever attached, however many
// requests were deferred; the oldest session stamp is kept for the recovery
// check.
let deferredLoginBefore: SessionStamp | null = null;
let visibilityListenerAttached = false;

function deferLoginRedirect(before: SessionStamp): void {
  deferredLoginBefore ??= before;
  if (visibilityListenerAttached) {
    return;
  }
  visibilityListenerAttached = true;
  const onVisible = (): void => {
    if (document.visibilityState !== "visible") {
      return;
    }
    document.removeEventListener("visibilitychange", onVisible);
    visibilityListenerAttached = false;
    const stamp = deferredLoginBefore;
    deferredLoginBefore = null;
    // Another tab may have published a valid session after our handoff
    // timeout expired; do not kick the user to login if the session recovered.
    const now = sessionStamp();
    if (stamp && !!now.access && (now.access !== stamp.access || now.refresh !== stamp.refresh)) {
      return;
    }
    redirectToLogin();
  };
  document.addEventListener("visibilitychange", onVisible);
}

// Another tab logged out: leave this one too, deferring while it is hidden so
// an inactive tab does not start an OAuth flow.
function onSessionClearedElsewhere(): void {
  if (getTokens()) {
    // A session exists again (e.g. logged back in); nothing to do.
    return;
  }
  if (document.visibilityState === "hidden") {
    deferLoginRedirect({ access: undefined, refresh: undefined });
    return;
  }
  window.location.href = "/";
}

let authSyncStarted = false;

// initAuthSync listens for logouts in other tabs. A tab's own BroadcastChannel
// never receives its own messages and the storage event only fires in other
// tabs, so a tab never reacts to its own logout.
function initAuthSync(): void {
  if (authSyncStarted || typeof window === "undefined") {
    return;
  }
  authSyncStarted = true;
  try {
    if (typeof BroadcastChannel !== "undefined") {
      const channel = new BroadcastChannel(AUTH_CHANNEL_NAME);
      channel.onmessage = (event: MessageEvent) => {
        if (event.data?.type === "session_cleared" && event.data.sender !== TAB_ID) {
          onSessionClearedElsewhere();
        }
      };
    }
  } catch {
    /* BroadcastChannel not available */
  }
  window.addEventListener("storage", (event: StorageEvent) => {
    if (event.key === AUTH_STORAGE_KEY && event.newValue === null) {
      onSessionClearedElsewhere();
    }
  });
}

initAuthSync();

export async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const applyAuth = (init?: RequestInit): RequestInit => {
    const tokens = getTokens();
    if (!tokens?.access_token) {
      return init ?? {};
    }
    const headers = new Headers(init?.headers);
    headers.set("Authorization", `Bearer ${tokens.access_token}`);
    return { ...init, headers };
  };

  let response = await fetch(input, applyAuth(init));
  if (response.status === 403) {
    // Authenticated but not authorized (missing required role). Refreshing or
    // logging in again cannot help, so go to the denial page instead of
    // bouncing through the OAuth flow (which loops and can fail the callback).
    redirectToDenied();
    return never();
  }
  if (response.status === 401) {
    // Tokens may be expired: try one refresh, then retry once.
    const before = sessionStamp();
    const result = await refreshOnce();
    if (result === "ok") {
      response = await fetch(input, applyAuth(init));
    } else if (result === "expired") {
      if (await sessionRefreshedElsewhere(before)) {
        // Another tab exchanged the single-use refresh token first; reuse the
        // session it published instead of forcing a re-login.
        response = await fetch(input, applyAuth(init));
      } else {
        // When running in a background tab (e.g. background polling), do not
        // bounce the page to /auth/login. An inactive tab cannot complete the
        // OAuth flow and would cause concurrent callback code exchanges or
        // state invalidations with active tabs. Complete the deferred redirect
        // once the user actually looks at this tab, unless another tab has
        // since restored a healthy session.
        if (typeof document !== "undefined" && document.visibilityState === "hidden") {
          deferLoginRedirect(before);
          return sharedNever;
        }
        // No valid, renewable session; start the OAuth login flow, returning to
        // the current page afterwards.
        redirectToLogin();
        return never();
      }
    }
    // "unavailable": keep the session and surface the response to the caller.
  }
  return response;
}

export class ApiError extends Error {
  status?: number;
  constructor(message: string, status?: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError && e.status === 409) {
    return "Already exists.";
  }
  return e instanceof Error && e.message ? e.message : "Unexpected error";
}

// seg encodes a single URL path segment.
export function seg(value: string): string {
  return encodeURIComponent(value);
}

// buildUrl appends query parameters with proper encoding, skipping empty values.
export function buildUrl(
  path: string,
  params?: Record<string, string | number | undefined>,
): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params ?? {})) {
    if (value !== undefined && value !== "") {
      query.set(key, String(value));
    }
  }
  const qs = query.toString();
  return qs ? `${path}?${qs}` : path;
}

// request performs an API call and throws ApiError on network or HTTP failure.
async function request(url: string, init?: RequestInit): Promise<Response> {
  let response: Response;
  try {
    response = await apiFetch(url, init);
  } catch {
    throw new ApiError("Network error");
  }
  if (!response.ok) {
    throw new ApiError(`HTTP error ${response.status}`, response.status);
  }
  return response;
}

async function getJSON<T>(url: string): Promise<T> {
  const response = await request(url);
  try {
    return (await response.json()) as T;
  } catch {
    throw new ApiError("Invalid response from server");
  }
}

export async function fetchIndexers(): Promise<string[]> {
  return getJSON<string[]>("/api/v1/indexers");
}

export interface Category {
  id: string;
  name: string;
  subCategories: Category[];
}

export async function fetchIndexerCategories(indexer: string): Promise<Category[]> {
  return getJSON<Category[]>(`/api/v1/indexers/${seg(indexer)}/categories`);
}

export interface DB {
  db: string;
  link: string;
  rating: string;
}

export interface Resource {
  id: string;
  title: string;
  title2: string;
  createdDate: number;
  category: string;
  size: number;
  resolution: string;
  seeders: number;
  leechers: number;
  dbs: DB[];
  images: string[];
  free: boolean;
  labels: string[];
  detailsUrl?: string;
}

export interface Pagination {
  page: number;
  totalPages: number;
  pageSize: number;
  total: number;
}

export interface ResourcesResponse {
  pagination: Pagination;
  resources: Resource[];
}

export async function fetchIndexerResources(
  indexer: string,
  category: string,
  keyword: string,
  page: number,
  pageSize: number = 100,
): Promise<ResourcesResponse> {
  return getJSON<ResourcesResponse>(
    buildUrl(`/api/v1/indexers/${seg(indexer)}/resources`, { category, keyword, page, pageSize }),
  );
}

export interface DownloaderInfo {
  name: string;
  count_of_downloading: number;
  count_of_planned: number;
  count_of_failed: number;
}

export async function fetchDownloaders(): Promise<DownloaderInfo[]> {
  return getJSON<DownloaderInfo[]>("/api/v1/downloaders");
}

export interface DownloadItem {
  id: string;
  created_at: string;
  updated_at: string;
  downloader: string;
  download_progress: number;
  state: number;
  res_indexer: string;
  res_title: string;
  res_title2: string;
  category: string;
  details_url?: string;
  file_list: string[];
  metadata: {
    actors: string[];
    category: string;
    description: string;
    dmm_id: string;
    labels: string[];
    organizer_category: string[];
    title: string;
  };
  size?: number;
  move_state: number;
  organize_state: number;
  organize_plans: PlanResponse | null;
}

export interface DownloaderState {
  count_of_downloading: number;
  count_of_planned: number;
  count_of_failed: number;
}

export interface DownloaderStatusResponse {
  state: DownloaderState;
  resources: DownloadItem[];
}

export type DownloadState = "downloading" | "seeding" | "stopped" | "planned" | "failed";

export async function fetchDownloaderItems(
  downloaderName: string,
  state: DownloadState,
): Promise<DownloaderStatusResponse> {
  const data = await getJSON<Partial<DownloaderStatusResponse>>(
    buildUrl(`/api/v1/downloaders/${seg(downloaderName)}`, { state }),
  );
  // Ensure the response has the expected structure
  return {
    state: data.state || { count_of_downloading: 0, count_of_planned: 0, count_of_failed: 0 },
    resources: data.resources || [],
  };
}

export type ActionType = "move" | "skip";

export interface PlanAction {
  file: string; // Exact original path
  action: ActionType; // "move" or "skip"
  target?: string; // Target path for "move" action
}

export interface PlanResponse {
  plan?: PlanAction[];
  error?: string;
}

export type OrganizeAction = "accept_plan" | "manual_organized" | "re_plan";

export async function organizeDownload(
  downloadId: string,
  action: OrganizeAction,
  userHint?: string,
): Promise<void> {
  await request(
    buildUrl(`/api/v1/download/${seg(downloadId)}/organize`, {
      action,
      user_hint: action === "re_plan" ? userHint : undefined,
    }),
    { method: "POST" },
  );
}

export async function deleteDownload(downloadId: string): Promise<void> {
  await request(`/api/v1/download/${seg(downloadId)}`, { method: "DELETE" });
}

// downloadResource throws ApiError; status 409 means it is already downloading.
export async function downloadResource(indexerId: string, resourceId: string): Promise<void> {
  await request(`/api/v1/indexers/${seg(indexerId)}/resources/${seg(resourceId)}/download`, {
    method: "POST",
  });
}
