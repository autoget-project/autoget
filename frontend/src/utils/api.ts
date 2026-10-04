const AUTH_STORAGE_KEY = "autoget_auth";
const REFRESH_LOCK = "autoget_auth_refresh";
// Cross-tab refresh lock kept in localStorage for browsers/site origins where
// the Web Locks API is unavailable (it is restricted to secure contexts). See
// acquireTabLock.
const TAB_LOCK_KEY = "autoget_auth_refresh_lock";
// A tab that holds the lock is assumed to have crashed once it is older than
// this, so a crashed holder cannot deadlock every other tab forever. It must
// comfortably exceed the time a single refresh takes.
const TAB_LOCK_TTL_MS = 10_000;
// How long a contender waits for the current holder before refreshing itself.
const TAB_LOCK_WAIT_MS = 10_000;
// Settle time before a contender confirms it won the lock; concurrent writers
// surface their write within this window.
const TAB_LOCK_SETTLE_MS = 50;
// A refresh token is single-use, so when two tabs exchange the same token at
// once one of them is rejected even though the session is healthy. This bounds
// how long the loser waits for the winner to publish its tokens before it
// treats the session as gone.
const SESSION_HANDOFF_TIMEOUT_MS = 1000;

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

function setTokens(tokens: StoredTokens): void {
  localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(tokens));
}

// isAuthenticated reports whether a session is stored locally. It is only true
// when auth is enabled and the user has logged in.
export function isAuthenticated(): boolean {
  return getTokens() !== null;
}

export function logout(): void {
  localStorage.removeItem(AUTH_STORAGE_KEY);
  // Reloading drops back to the login flow via the 401 handling below.
  window.location.href = "/";
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
    });
  } catch {
    // Network failure: the tokens may still be valid, so keep the session.
    return "unavailable";
  }

  if (response.ok) {
    setTokens((await response.json()) as StoredTokens);
    return "ok";
  }
  if (response.status === 400 || response.status === 401) {
    // The refresh token is gone; the user has to log in again.
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
// cross-tab storage event (bounded) before giving up.
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
    const finish = (value: boolean): void => {
      if (settled) {
        return;
      }
      settled = true;
      window.removeEventListener("storage", onStorage);
      clearTimeout(timer);
      resolve(value);
    };
    const onStorage = (event: StorageEvent): void => {
      if (event.key === AUTH_STORAGE_KEY && changed()) {
        finish(true);
      }
    };
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

function writeTabLock(lock: TabLock): void {
  try {
    localStorage.setItem(TAB_LOCK_KEY, JSON.stringify(lock));
  } catch {
    /* storage unavailable (e.g. private mode): fall back to the in-page flight */
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
      writeTabLock(me);
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
    // Return an unresolved promise so callers neither retry nor start an
    // endless fetch loop while the browser navigates away.
    return new Promise(() => {});
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
        // No valid, renewable session; start the OAuth login flow, returning to
        // the current page afterwards.
        redirectToLogin();
        // Return an unresolved promise so downstream code doesn't throw or trigger error toasts during navigation
        return new Promise(() => {});
      }
    }
    // "unavailable": keep the session and surface the response to the caller.
  }
  return response;
}

export async function fetchIndexers(): Promise<string[]> {
  try {
    const response = await apiFetch("/api/v1/indexers");
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error("Failed to fetch indexers:", error);
    return []; // Set to empty array on error
  }
}

export interface Category {
  id: string;
  name: string;
  subCategories: Category[];
}

export async function fetchIndexerCategories(indexer: string): Promise<Category[]> {
  try {
    const response = await apiFetch(`/api/v1/indexers/${indexer}/categories`);
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error("Failed to fetch indexers:", error);
    return []; // Set to empty array on error
  }
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
): Promise<ResourcesResponse | null> {
  try {
    const response = await apiFetch(
      `/api/v1/indexers/${indexer}/resources?category=${category}&keyword=${keyword}&page=${page}&pageSize=${pageSize}`,
    );
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error("Failed to fetch indexer resources:", error);
    return null;
  }
}

export interface DownloaderInfo {
  name: string;
  count_of_downloading: number;
  count_of_planned: number;
  count_of_failed: number;
}

export async function fetchDownloaders(): Promise<DownloaderInfo[]> {
  try {
    const response = await apiFetch("/api/v1/downloaders");
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error("Failed to fetch downloaders:", error);
    return []; // Set to empty array on error
  }
}

export interface DownloadItem {
  ID: string;
  CreatedAt: string;
  UpdatedAt: string;
  Downloader: string;
  DownloadProgress: number;
  State: number;
  ResIndexer: string;
  ResTitle: string;
  ResTitle2: string;
  Category: string;
  DetailsURL?: string;
  FileList: string[];
  Metadata: {
    actors: string[];
    category: string;
    description: string;
    dmm_id: string;
    labels: string[];
    organizer_category: string[];
    title: string;
  };
  Size?: number;
  MoveState: number;
  OrganizeState: number;
  OrganizePlans: PlanResponse | null;
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
  try {
    const response = await apiFetch(`/api/v1/downloaders/${downloaderName}?state=${state}`);
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    const data = await response.json();

    // Ensure the response has the expected structure
    return {
      state: data.state || { count_of_downloading: 0, count_of_planned: 0, count_of_failed: 0 },
      resources: data.resources || [],
    };
  } catch (error) {
    console.error("Failed to fetch downloader items:", error);
    return {
      state: { count_of_downloading: 0, count_of_planned: 0, count_of_failed: 0 },
      resources: [],
    };
  }
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
): Promise<boolean> {
  try {
    let url = `/api/v1/download/${downloadId}/organize?action=${action}`;
    if (userHint && action === "re_plan") {
      url += `&user_hint=${encodeURIComponent(userHint)}`;
    }

    const response = await apiFetch(url, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return true;
  } catch (error) {
    console.error("Failed to organize download:", error);
    return false;
  }
}

export async function deleteDownload(downloadId: string): Promise<boolean> {
  try {
    const response = await apiFetch(`/api/v1/download/${downloadId}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return true;
  } catch (error) {
    console.error("Error deleting download:", error);
    return false;
  }
}

export async function downloadResource(indexerId: string, resourceId: string): Promise<boolean> {
  try {
    const response = await apiFetch(
      `/api/v1/indexers/${indexerId}/resources/${resourceId}/download`,
      {
        method: "GET",
      },
    );
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return true;
  } catch (error) {
    console.error("Error initiating download:", error);
    return false;
  }
}
