// safeExternalUrl returns value when it is an absolute http(s) URL.
//
// Values such as db.link come from third-party indexers, so a bare
// javascript: URL must never end up in an href. Anything that is not a plain
// http(s) URL (javascript:, data:, relative, malformed) is rejected.
export function safeExternalUrl(value: string | undefined | null): string | undefined {
  if (!value) {
    return undefined;
  }

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return undefined;
  }

  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return undefined;
  }
  return parsed.href;
}
