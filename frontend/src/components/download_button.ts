import { html, LitElement, css, type PropertyValues, type TemplateResult, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import globalStyles from "/src/index.css?inline";
import { ApiError, downloadResource, errorMessage } from "../utils/api.js";

// Download outcomes for this page session (until reload), keyed by
// `${indexerId}/${resourceId}`. Buttons are reused across relayouts and page
// changes, and several buttons can show the same resource, so state lives here.
const outcomes = new Map<string, "added" | "duplicate">();

@customElement("download-button")
export class DownloadButton extends LitElement {
  static styles = [
    unsafeCSS(globalStyles),
    css`
      :host {
        display: inline-block;
      }
    `,
  ];

  @property({ type: String })
  public indexerId: string = "";

  @property({ type: String })
  public resourceId: string = "";

  @state()
  private isLoading: boolean = false;

  @state()
  private isAdded: boolean = false;

  @state()
  private hasFailed: boolean = false;

  @state()
  private isDuplicate: boolean = false;

  @state()
  private errorText: string = "";

  private get key(): string {
    return `${this.indexerId}/${this.resourceId}`;
  }

  protected willUpdate(changed: PropertyValues): void {
    if (!changed.has("indexerId") && !changed.has("resourceId")) return;
    const outcome = outcomes.get(this.key);
    this.isAdded = outcome === "added";
    this.isDuplicate = outcome === "duplicate";
    this.hasFailed = false;
    this.errorText = "";
    this.isLoading = false;
  }

  private async handleDownloadClick() {
    if (this.isLoading || this.isAdded) {
      return;
    }

    const key = this.key;
    this.isLoading = true;
    this.hasFailed = false;
    this.requestUpdate();

    try {
      await downloadResource(this.indexerId, this.resourceId);
      outcomes.set(key, "added");
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        // The backend already tracks this torrent; nothing to retry.
        outcomes.set(key, "duplicate");
      } else if (key === this.key) {
        console.error("Error initiating download:", error);
        this.errorText = errorMessage(error);
        this.hasFailed = true;
      }
    } finally {
      if (key === this.key) {
        const outcome = outcomes.get(key);
        this.isAdded = outcome === "added";
        this.isDuplicate = outcome === "duplicate";
        this.isLoading = false;
        this.requestUpdate();
      }
    }
  }

  private renderButton(): TemplateResult {
    if (this.isDuplicate) {
      return html`
        <button class="btn btn-xs btn-warning" disabled title="Already in your downloads">
          Already added
        </button>
      `;
    }
    if (this.isLoading) {
      // Show loading spinner
      return html`
        <button class="btn btn-xs btn-info" disabled role="status" aria-live="polite">
          <span class="loading loading-spinner loading-xs" aria-hidden="true"></span>
          <span class="sr-only">Loading...</span>
        </button>
      `;
    } else if (this.isAdded) {
      // Show added state
      return html` <button class="btn btn-xs btn-success" disabled>Added</button> `;
    } else if (this.hasFailed) {
      // Show failed state - allow retry
      return html`
        <button
          class="btn btn-xs btn-error"
          title=${this.errorText}
          @click=${this.handleDownloadClick}
        >
          Failed
        </button>
      `;
    } else {
      // Show normal download button
      return html`
        <button class="btn btn-xs btn-info" @click=${this.handleDownloadClick}>Download</button>
      `;
    }
  }

  render() {
    return html`${this.renderButton()}`;
  }
}
