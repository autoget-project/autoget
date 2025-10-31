import { html, LitElement, css, type TemplateResult, unsafeCSS } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import globalStyles from '/src/index.css?inline';

@customElement('download-button')
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
  public indexerId: string = '';

  @property({ type: String })
  public resourceId: string = '';

  @state()
  private isLoading: boolean = false;

  @state()
  private isAdded: boolean = false;

  @state()
  private hasFailed: boolean = false;

  private async handleDownloadClick() {
    if (this.isLoading || this.isAdded) {
      return;
    }

    this.isLoading = true;
    this.hasFailed = false;
    this.requestUpdate();

    const url = `/api/v1/indexers/${this.indexerId}/resources/${this.resourceId}/download`;
    try {
      const response = await fetch(url, { method: 'GET' });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      // On success, show added state
      this.isAdded = true;
    } catch (error) {
      console.error('Error initiating download:', error);
      // On error, show failed state
      this.hasFailed = true;
    } finally {
      this.isLoading = false;
      this.requestUpdate();
    }
  }

  private renderButton(): TemplateResult {
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
      return html` <button class="btn btn-xs btn-error" @click=${this.handleDownloadClick}>Failed</button> `;
    } else {
      // Show normal download button
      return html` <button class="btn btn-xs btn-info" @click=${this.handleDownloadClick}>Download</button> `;
    }
  }

  render() {
    return html`${this.renderButton()}`;
  }
}
