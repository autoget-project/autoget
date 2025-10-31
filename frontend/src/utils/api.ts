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
