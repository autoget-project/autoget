export async function fetchIndexers(): Promise<string[]> {
  try {
    const response = await fetch('/api/v1/indexers');
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error('Failed to fetch indexers:', error);
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
    const response = await fetch(`/api/v1/indexers/${indexer}/categories`);
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error('Failed to fetch indexers:', error);
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
    const response = await fetch(
      `/api/v1/indexers/${indexer}/resources?category=${category}&keyword=${keyword}&page=${page}&pageSize=${pageSize}`,
    );
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error('Failed to fetch indexer resources:', error);
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
    const response = await fetch('/api/v1/downloaders');
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error('Failed to fetch downloaders:', error);
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

export type DownloadState = 'downloading' | 'seeding' | 'stopped' | 'planned' | 'failed';

export async function fetchDownloaderItems(
  downloaderName: string,
  state: DownloadState,
): Promise<DownloaderStatusResponse> {
  try {
    const response = await fetch(`/api/v1/downloaders/${downloaderName}?state=${state}`);
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
    console.error('Failed to fetch downloader items:', error);
    return { state: { count_of_downloading: 0, count_of_planned: 0, count_of_failed: 0 }, resources: [] };
  }
}

export type ActionType = 'move' | 'skip';

export interface PlanAction {
  file: string; // Exact original path
  action: ActionType; // "move" or "skip"
  target?: string; // Target path for "move" action
}

export interface PlanResponse {
  plan?: PlanAction[];
  error?: string;
}

export type OrganizeAction = 'accept_plan' | 'manual_organized' | 're_plan';

export async function organizeDownload(
  downloadId: string,
  action: OrganizeAction,
  userHint?: string,
): Promise<boolean> {
  try {
    let url = `/api/v1/download/${downloadId}/organize?action=${action}`;
    if (userHint && action === 're_plan') {
      url += `&user_hint=${encodeURIComponent(userHint)}`;
    }

    const response = await fetch(url, {
      method: 'POST',
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return true;
  } catch (error) {
    console.error('Failed to organize download:', error);
    return false;
  }
}

export async function deleteDownload(downloadId: string): Promise<boolean> {
  try {
    const response = await fetch(`/api/v1/download/${downloadId}`, {
      method: 'DELETE',
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return true;
  } catch (error) {
    console.error('Error deleting download:', error);
    return false;
  }
}

export async function downloadResource(indexerId: string, resourceId: string): Promise<boolean> {
  try {
    const response = await fetch(`/api/v1/indexers/${indexerId}/resources/${resourceId}/download`, {
      method: 'GET',
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return true;
  } catch (error) {
    console.error('Error initiating download:', error);
    return false;
  }
}
