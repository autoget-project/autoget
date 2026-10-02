const AUTH_STORAGE_KEY = "autoget_auth";

interface StoredTokens {
  access_token: string;
  refresh_token: string;
  id_token: string;
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

export function logout(): void {
  localStorage.removeItem(AUTH_STORAGE_KEY);
  window.location.href = "/";
}

let refreshing: Promise<boolean> | null = null;

// refreshTokens exchanges the stored refresh token for new tokens via the
// backend (which holds the client secret). Returns true on success.
async function refreshTokens(): Promise<boolean> {
  const tokens = getTokens();
  if (!tokens?.refresh_token) {
    return false;
  }
  try {
    const response = await fetch("/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: tokens.refresh_token }),
    });
    if (!response.ok) {
      return false;
    }
    setTokens(await response.json());
    return true;
  } catch {
    return false;
  }
}

function redirectToLogin(): void {
  const current = new URL(window.location.href);
  window.location.href = `/auth/login?redirect=${encodeURIComponent(current.pathname + current.search)}`;
}

async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
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
  if (response.status === 401) {
    // tokens may be expired: try one refresh, then retry once
    refreshing ??= refreshTokens().finally(() => {
      refreshing = null;
    });
    if (await refreshing) {
      response = await fetch(input, applyAuth(init));
    }
  }
  if (response.status === 401) {
    // no valid/renewable tokens; go through the OAuth login flow,
    // returning to the current page afterwards
    redirectToLogin();
    // Return an unresolved promise so downstream code doesn't throw or trigger error toasts during navigation
    return new Promise(() => {});
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
