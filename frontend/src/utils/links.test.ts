import { describe, it, expect } from "vitest";
import { externalLink } from "./links";

describe("externalLink", () => {
  it("renders an anchor with hardened target/rel attributes for a safe URL", () => {
    const result = externalLink({ url: "https://example.com/view/1", content: "x" });
    if (!result) {
      throw new Error("expected a template result for a safe URL");
    }
    const markup = result.strings.join("");
    expect(markup).toContain('target="_blank"');
    expect(markup).toContain('rel="noopener noreferrer"');
  });

  it("returns undefined so callers can fall back for missing or unsafe URLs", () => {
    expect(externalLink({ url: undefined, content: "x" })).toBeUndefined();
    expect(externalLink({ url: "javascript:alert(1)", content: "x" })).toBeUndefined();
    expect(externalLink({ url: "/relative", content: "x" })).toBeUndefined();
  });
});
