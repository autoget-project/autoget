import { html, type TemplateResult } from "lit";

// renderError renders an inline error alert with an optional retry button.
export function renderError(
  message: string,
  onRetry?: () => void,
  onDismiss?: () => void,
): TemplateResult {
  return html`
    <div class="alert alert-error" role="alert">
      <span class="icon-[ph--x-bold]" style="width: 1.2em; height: 1.2em;"></span>
      <span>${message}</span>
      ${onRetry ? html`<button class="btn btn-sm" @click=${onRetry}>Retry</button>` : ""}
      ${
        onDismiss
          ? html`<button class="btn btn-sm btn-ghost" aria-label="Dismiss" @click=${onDismiss}>
              ✕
            </button>`
          : ""
      }
    </div>
  `;
}
