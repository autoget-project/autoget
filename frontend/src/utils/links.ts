import { html, nothing, type TemplateResult } from "lit";
import { safeExternalUrl } from "./url";

export interface ExternalLinkOptions {
  /** Candidate URL; rendered only when it is a valid absolute http(s) URL. */
  url: string | undefined | null;
  /** Anchor content. */
  content: TemplateResult | string;
  /** Classes applied to the anchor. */
  class?: string;
  /** Tooltip text. */
  title?: string;
  /** Accessible name; use it to keep concurrent links distinguishable. */
  label?: string;
}

/**
 * Renders an anchor pointing at a validated external http(s) URL, or undefined
 * when the URL is missing or unsafe so the caller can render a non-link
 * fallback.
 *
 * Centralising this keeps the security-relevant `target`/`rel` attributes and
 * the scheme whitelist in one place: no caller can forget them.
 */
export function externalLink({
  url,
  content,
  class: className,
  title,
  label,
}: ExternalLinkOptions): TemplateResult | undefined {
  const href = safeExternalUrl(url);
  if (!href) {
    return undefined;
  }

  return html`<a
    href="${href}"
    target="_blank"
    rel="noopener noreferrer"
    class="${className ?? ""}"
    title=${title ?? nothing}
    aria-label=${label ?? nothing}
    >${content}</a
  >`;
}
