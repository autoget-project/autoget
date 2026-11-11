import { html, LitElement, css, type TemplateResult, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import globalStyles from "/src/index.css?inline";
import { ApiError, downloadResource, errorMessage } from "../utils/api.js";

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

  private async handleDownloadClick() {
    if (this.isLoading || this.isAdded) {
      return;
    }

    this.isLoading = true;
    this.hasFailed = false;
    this.requestUpdate();

    try {
      await downloadResource(this.indexerId, this.resourceId);
      this.isAdded = true;
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        // The backend already tracks this torrent; nothing to retry.
        this.isDuplicate = true;
      } else {
        console.error("Error initiating download:", error);
        this.errorText = errorMessage(error);
        this.hasFailed = true;
      }
    } finally {
      this.isLoading = false;
      this.requestUpdate();
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
        <button class="btn btn-xs btn-info" disabled>
          <span class="loading loading-spinner loading-xs"></span>
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
