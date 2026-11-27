import { html, LitElement, nothing, unsafeCSS, css, type TemplateResult } from "lit";
import { customElement, state, query } from "lit/decorators.js";

import {
  errorMessage,
  fetchResourceDetail,
  type Resource,
  type ResourceDetail,
} from "../utils/api";
import { renderError } from "./error_alert";
import { formatBytes } from "../utils/format";
import { externalLink } from "../utils/links";
import globalStyles from "/src/index.css?inline";
import "./download_button.js";

@customElement("resource-detail-dialog")
export class ResourceDetailDialog extends LitElement {
  static styles = [
    unsafeCSS(globalStyles),
    css`
      dialog:modal {
        overscroll-behavior: contain;
      }
      dialog::backdrop {
        overflow: hidden;
        overscroll-behavior: contain;
      }
    `,
  ];

  @query("dialog")
  private dialog!: HTMLDialogElement;

  @state()
  private indexerId = "";

  @state()
  private resource: Resource | null = null;

  @state()
  private detail: ResourceDetail | null = null;

  @state()
  private isLoading = false;

  @state()
  private error: string | null = null;

  private requestSeq = 0;

  // Shows the list data at once, then loads the detail.
  public open(indexerId: string, resource: Resource): void {
    this.indexerId = indexerId;
    this.resource = resource;
    this.detail = null;
    void this.load();
    if (!this.dialog.open) this.dialog.showModal();
  }

  private retry = () => {
    void this.load();
  };

  private async load() {
    const seq = ++this.requestSeq;
    const id = this.resource?.id;
    if (!id) return;
    this.isLoading = true;
    this.error = null;
    try {
      const detail = await fetchResourceDetail(this.indexerId, id);
      if (seq !== this.requestSeq) return;
      this.detail = detail;
    } catch (error) {
      if (seq !== this.requestSeq) return;
      console.error("Failed to fetch resource detail:", error);
      this.error = errorMessage(error);
    }
    this.isLoading = false;
  }

  private close = () => {
    this.dialog.close();
  };

  private handleClose = () => {
    // Invalidate any in-flight request once the dialog is dismissed.
    this.requestSeq++;
  };

  private renderFiles(detail: ResourceDetail): TemplateResult | typeof nothing {
    const files = detail.files ?? [];
    if (files.length === 0) return nothing;
    const total = files.reduce((sum, f) => sum + f.size, 0);
    return html`
      <h4 class="font-medium mt-3 mb-1">Files (${files.length}, ${formatBytes(total)})</h4>
      <div class="max-h-64 overflow-auto">
        <table class="table table-xs">
          <tbody>
            ${files.map(
              (f) =>
                html`<tr>
                  <td class="break-all">${f.name}</td>
                  <td class="whitespace-nowrap text-right">${formatBytes(f.size)}</td>
                </tr>`,
            )}
          </tbody>
        </table>
      </div>
    `;
  }

  // Text only: BBCode and markdown stay raw, never rendered as HTML.
  private renderText(title: string, text: string | undefined): TemplateResult | typeof nothing {
    if (!text) return nothing;
    return html`
      <details class="collapse collapse-arrow bg-base-200 mt-3">
        <summary class="collapse-title font-medium">${title}</summary>
        <div class="collapse-content">
          <pre class="text-xs whitespace-pre-wrap break-all">${text}</pre>
        </div>
      </details>
    `;
  }

  private renderBody(r: Resource): TemplateResult {
    const detail = this.detail;
    const images = detail?.images ?? [];
    return html`
      <div class="flex flex-wrap gap-1 mt-2">
        <span class="badge badge-outline badge-primary">${r.category}</span>
        <span class="badge badge-outline badge-secondary">${formatBytes(r.size)}</span>
        ${
          r.resolution
            ? html`<span class="badge badge-outline badge-info">${r.resolution}</span>`
            : nothing
        }
        ${r.free ? html`<span class="badge badge-success">Free</span>` : nothing}
        <span class="badge badge-info">
          <span class="icon-[icons8--up-round]"></span>
          ${r.seeders}
        </span>
        <span class="badge badge-neutral">${r.leechers}</span>
      </div>
      ${
        this.isLoading
          ? html`<div class="flex justify-center py-6" role="status" aria-live="polite">
              <span class="loading loading-spinner loading-lg" aria-hidden="true"></span>
              <span class="sr-only">Loading...</span>
            </div>`
          : nothing
      }
      ${this.error ? html`<div class="mt-3">${renderError(this.error, this.retry)}</div>` : nothing}
      ${
        detail
          ? html`
              ${
                images.length > 0
                  ? html`<div class="flex overflow-x-auto gap-2 mt-3">
                      ${images.map(
                        (src) =>
                          html`<img
                            src="${src}"
                            alt=""
                            class="h-48 w-auto rounded"
                            loading="lazy"
                          />`,
                      )}
                    </div>`
                  : nothing
              }
              ${this.renderFiles(detail)} ${this.renderText("Mediainfo", detail.mediainfo)}
              ${this.renderText("Description", detail.description)}
            `
          : nothing
      }
    `;
  }

  render() {
    const r = this.resource;
    return html`
      <dialog
        class="modal modal-bottom sm:modal-middle backdrop-blur-xs"
        closedby="any"
        aria-labelledby="detail-title"
        @close=${this.handleClose}
      >
        <div class="modal-box max-w-4xl">
          ${
            r
              ? html`
                  <div class="flex items-start justify-between gap-2">
                    <div class="min-w-0">
                      <h3 id="detail-title" class="font-bold text-lg break-all">
                        ${r.title || "Untitled Resource"}
                      </h3>
                      ${
                        r.title2
                          ? html`<p class="text-base-content/80 break-all">${r.title2}</p>`
                          : nothing
                      }
                    </div>
                    <button
                      class="btn btn-sm btn-circle btn-ghost"
                      aria-label="Close"
                      @click=${this.close}
                    >
                      ✕
                    </button>
                  </div>
                  ${this.renderBody(r)}
                  <div class="modal-action items-center">
                    ${
                      externalLink({
                        url: r.detailsUrl,
                        class: "btn btn-sm btn-ghost",
                        label: "Open on indexer",
                        content: "Open on indexer",
                      }) ?? nothing
                    }
                    <download-button
                      indexerId="${this.indexerId}"
                      resourceId="${r.id}"
                    ></download-button>
                  </div>
                `
              : nothing
          }
        </div>
        <form method="dialog" class="modal-backdrop">
          <button type="submit">close</button>
        </form>
      </dialog>
    `;
  }
}
