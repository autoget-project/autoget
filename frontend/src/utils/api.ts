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

export async function fetchDownloaders(): Promise<string[]> {
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
  MoveState: number;
  OrganizeState: number;
  OrganizePlans: PlanResponse | null;
}

export type DownloadState = 'downloading' | 'seeding' | 'stopped' | 'planned';

export async function fetchDownloaderItems(downloaderName: string, state: DownloadState): Promise<DownloadItem[]> {
  try {
    const response = await fetch(`/api/v1/downloaders/${downloaderName}?state=${state}`);
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error('Failed to fetch downloader items:', error);
    return []; // Set to empty array on error
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
