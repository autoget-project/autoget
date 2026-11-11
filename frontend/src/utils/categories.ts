import type { Category } from "./api.ts";

export function categoryLevelTitle(level: number): string {
  return level === 0 ? "Main Category" : level === 1 ? "Sub Category" : "Tertiary Category";
}

/** Depth-first search for the path of categories from the root to targetId, or null if absent. */
export function findCategoryPath(
  categories: Category[],
  targetId: string,
  currentPath: Category[] = [],
): Category[] | null {
  for (const cat of categories) {
    const newPath = [...currentPath, cat];
    if (cat.id === targetId) {
      return newPath;
    }
    if (cat.subCategories) {
      const subPath = findCategoryPath(cat.subCategories, targetId, newPath);
      if (subPath) {
        return subPath;
      }
    }
  }
  return null;
}

/** The category list shown at each level for a selected path (one entry per path element). */
export function levelsForPath(allCategories: Category[], path: Category[]): Category[][] {
  const levels: Category[][] = [];
  let current = allCategories;
  for (const selected of path) {
    levels.push(current);
    current = selected.subCategories || [];
  }
  return levels;
}
