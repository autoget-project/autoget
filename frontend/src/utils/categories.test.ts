import { describe, it, expect } from "vitest";
import type { Category } from "./api.ts";
import { categoryLevelTitle, findCategoryPath, levelsForPath } from "./categories.ts";

const tree: Category[] = [
  {
    id: "1",
    name: "Movies",
    subCategories: [
      { id: "11", name: "HD", subCategories: [{ id: "111", name: "4K", subCategories: [] }] },
      { id: "12", name: "SD", subCategories: [] },
    ],
  },
  { id: "2", name: "TV", subCategories: [] },
];

describe("categoryLevelTitle", () => {
  it("names levels", () => {
    expect(categoryLevelTitle(0)).toBe("Main Category");
    expect(categoryLevelTitle(1)).toBe("Sub Category");
    expect(categoryLevelTitle(2)).toBe("Tertiary Category");
    expect(categoryLevelTitle(5)).toBe("Tertiary Category");
  });
});

describe("findCategoryPath", () => {
  it("finds nested paths", () => {
    expect(findCategoryPath(tree, "111")?.map((c) => c.id)).toEqual(["1", "11", "111"]);
    expect(findCategoryPath(tree, "2")?.map((c) => c.id)).toEqual(["2"]);
  });
  it("returns null when missing", () => {
    expect(findCategoryPath(tree, "nope")).toBeNull();
    expect(findCategoryPath([], "1")).toBeNull();
  });
});

describe("levelsForPath", () => {
  it("returns one level per path element", () => {
    const path = findCategoryPath(tree, "11")!;
    const levels = levelsForPath(tree, path);
    expect(levels).toHaveLength(2);
    expect(levels[0]).toBe(tree);
    expect(levels[1]).toBe(tree[0].subCategories);
  });
  it("is empty for an empty path", () => {
    expect(levelsForPath(tree, [])).toEqual([]);
  });
});
