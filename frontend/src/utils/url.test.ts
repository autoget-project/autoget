import { describe, it, expect } from "vitest";
import { safeExternalUrl } from "./url";

describe("safeExternalUrl", () => {
  it("accepts absolute http and https URLs", () => {
    expect(safeExternalUrl("https://www.imdb.com/title/tt1234/")).toBe(
      "https://www.imdb.com/title/tt1234/",
    );
    expect(safeExternalUrl("http://example.com/x")).toBe("http://example.com/x");
  });

  it("rejects script and data URLs", () => {
    expect(safeExternalUrl("javascript:alert(1)")).toBeUndefined();
    expect(safeExternalUrl("JavaScript:alert(1)")).toBeUndefined();
    expect(safeExternalUrl("data:text/html,<script>alert(1)</script>")).toBeUndefined();
    expect(safeExternalUrl("vbscript:msgbox(1)")).toBeUndefined();
  });

  it("rejects relative and malformed values", () => {
    expect(safeExternalUrl("/indexers/mteam")).toBeUndefined();
    expect(safeExternalUrl("not a url")).toBeUndefined();
    expect(safeExternalUrl("")).toBeUndefined();
    expect(safeExternalUrl(undefined)).toBeUndefined();
    expect(safeExternalUrl(null)).toBeUndefined();
  });
});
