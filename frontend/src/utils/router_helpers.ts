import type { Category } from "./api.ts";

export interface ResolveIndexersOptions {
  maxAttempts?: number;
  baseDelayMs?: number;
  sleep?: (ms: number) => Promise<void>;
}

const defaultSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

// Fetches indexers with a bounded number of attempts and exponential backoff.
// Returns an empty list when every attempt fails or returns nothing.
export async function resolveIndexers(
  fetcher: () => Promise<string[]>,
  opts: ResolveIndexersOptions = {},
): Promise<string[]> {
  const { maxAttempts = 3, baseDelayMs = 500, sleep = defaultSleep } = opts;
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    try {
      const result = await fetcher();
      if (result.length > 0) {
        return result;
      }
    } catch {
      // treated as a failed attempt
    }
    if (attempt < maxAttempts - 1) {
      await sleep(baseDelayMs * 2 ** attempt);
    }
  }
  return [];
}

export function defaultCategoryId(categories: Category[]): string | undefined {
  return categories[0]?.id;
}
