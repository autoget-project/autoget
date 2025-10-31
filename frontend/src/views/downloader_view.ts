import { html, LitElement, unsafeCSS } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import '../components/navbar.ts';
import globalStyles from '/src/index.css?inline';
import {
  fetchDownloaderItems,
  organizeDownload,
  type DownloadItem,
  type DownloadState,
  type OrganizeAction,
  type PlanAction,
  type PlanResponse,
} from '../utils/api.ts';

@customElement('downloader-view')
export class DownloaderView extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @property({ type: String })
  public downloaderId: string = '';

  @state()
  private activeTab: string = 'downloading';

  @state()
  private downloadItems: DownloadItem[] = [];

  @state()
  private loading: boolean = false;

  @state()
  private error: string | null = null;

  private readonly tabs = [
    { id: 'downloading', label: 'Downloading' },
    { id: 'seeding', label: 'Seeding' },
    { id: 'stopped', label: 'Stopped' },
    { id: 'planned', label: 'Planned' },
  ];

  protected async firstUpdated() {
    await this.loadDownloadItems();
  }

  protected async updated(changedProperties: Map<string, unknown>) {
    if (changedProperties.has('activeTab') || changedProperties.has('downloaderId')) {
      await this.loadDownloadItems();
    }
  }

  private async loadDownloadItems() {
    if (!this.downloaderId) return;

    this.loading = true;
    this.error = null;

    try {
      this.downloadItems = await fetchDownloaderItems(this.downloaderId, this.activeTab as DownloadState);
    } catch (err) {
      this.error = err instanceof Error ? err.message : 'Failed to load download items';
      this.downloadItems = [];
    } finally {
      this.loading = false;
    }
  }

  private handleTabChange(tabId: string) {
    this.activeTab = tabId;
  }

  private formatDate(dateString: string): string {
    return new Date(dateString).toLocaleString();
  }

  private getMoveStateLabel(moveState: number): { label: string; color: string } {
    switch (moveState) {
      case 0: // UnMoved
        return { label: 'Unmoved', color: 'warning' };
      case 1: // Moved
        return { label: 'Moved', color: 'success' };
      default:
        return { label: 'Unknown', color: 'neutral' };
    }
  }

  private getOrganizeStateLabel(organizeState: number): { label: string; color: string } {
    switch (organizeState) {
      case 0: // Unplanned
        return { label: 'Unplanned', color: 'neutral' };
      case 1: // Planned
        return { label: 'Planned', color: 'info' };
      case 2: // Organized
        return { label: 'Organized', color: 'success' };
      case 3: // ExecutePlanFailed
        return { label: 'Plan Failed', color: 'error' };
      default:
        return { label: 'Unknown', color: 'neutral' };
    }
  }

  private async handleOrganizeAction(downloadId: string, action: OrganizeAction) {
    try {
      const success = await organizeDownload(downloadId, action);
      if (success) {
        // Refresh the data after successful action
        await this.loadDownloadItems();
      } else {
        // Show error message (could add a toast/notification here)
        console.error('Failed to organize download');
      }
    } catch (error) {
      console.error('Error organizing download:', error);
    }
  }

  private renderOrganizePlans(organizePlans: PlanResponse | null) {
    if (!organizePlans || !organizePlans.plan || organizePlans.plan.length === 0) {
      return html``;
    }

    if (organizePlans.error) {
      return html`
        <div class="alert alert-warning mt-4">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            class="stroke-current shrink-0 h-6 w-6"
            fill="none"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.732-.833-2.5 0L4.314 16.5c-.77.833.192 2.5 1.732 2.5z"
            />
          </svg>
          <span>Plan Error: ${organizePlans.error}</span>
        </div>
      `;
    }

    return html`
      <div class="mt-4">
        <div tabindex="0" class="collapse collapse-arrow bg-base-200">
          <input type="checkbox" />
          <div class="collapse-title text-lg font-medium">Organize Plan (${organizePlans.plan.length} items)</div>
          <div class="collapse-content">
            <div class="overflow-x-auto">
              <table class="table table-sm">
                <thead>
                  <tr>
                    <th>Action</th>
                    <th>Original Path</th>
                    <th>Target</th>
                  </tr>
                </thead>
                <tbody>
                  ${organizePlans.plan.map((planAction: PlanAction) => this.renderPlanAction(planAction))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  private renderPlanAction(planAction: PlanAction) {
    const actionBadge =
      planAction.action === 'move'
        ? html`<span class="badge badge-success">Move</span>`
        : html`<span class="badge badge-warning">Skip</span>`;

    const targetDisplay = planAction.action === 'move' ? planAction.target || 'No target specified' : 'skip';

    return html`
      <tr>
        <td>${actionBadge}</td>
        <td class="font-mono text-sm">${planAction.file}</td>
        <td class="font-mono text-sm">${targetDisplay}</td>
      </tr>
    `;
  }

  private renderDownloadItem(item: DownloadItem) {
    return html`
      <div class="card bg-base-100 shadow-sm mb-4">
        <div class="card-body">
          <div class="flex justify-between items-start">
            <div class="flex-1">
              <h3 class="card-title text-lg">${item.ResTitle}</h3>
              ${item.ResTitle2 ? html`<p class="text-sm text-base-content/70 mt-1">${item.ResTitle2}</p>` : ''}
              <div class="flex flex-wrap gap-2 mt-2">
                <span class="badge badge-neutral">${item.ResIndexer}</span>
                <span class="badge badge-outline">${item.Category}</span>
                <span class="badge badge-${this.getMoveStateLabel(item.MoveState).color}">
                  ${this.getMoveStateLabel(item.MoveState).label}
                </span>
                <span class="badge badge-${this.getOrganizeStateLabel(item.OrganizeState).color}">
                  ${this.getOrganizeStateLabel(item.OrganizeState).label}
                </span>
              </div>
              <div class="mt-3 text-sm text-base-content/60">
                <p>Created: ${this.formatDate(item.CreatedAt)}</p>
              </div>
              ${this.renderOrganizePlans(item.OrganizePlans)}
            </div>
            <div class="flex items-center gap-4">
              <div
                class="radial-progress"
                style="--value:${item.DownloadProgress};"
                aria-valuenow=${item.DownloadProgress}
                role="progressbar"
              >
                ${item.DownloadProgress}%
              </div>
              <div class="card-actions">
                ${this.activeTab === 'planned'
                  ? html`
                      <button
                        class="btn btn-sm btn-success"
                        @click=${() => this.handleOrganizeAction(item.ID, 'accept_plan')}
                      >
                        Accept Plan
                      </button>
                      <button
                        class="btn btn-sm btn-secondary"
                        @click=${() => this.handleOrganizeAction(item.ID, 'manual_organized')}
                      >
                        Mark Manual Organized
                      </button>
                    `
                  : html``}
              </div>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  private renderTabContent() {
    const tabTitles = {
      downloading: 'Currently Downloading',
      seeding: 'Seeding Torrents',
      stopped: 'Stopped Downloads',
      planned: 'Planned Downloads',
    };

    const emptyMessages = {
      downloading: 'No active downloads at the moment.',
      seeding: 'No torrents are currently seeding.',
      stopped: 'No stopped downloads.',
      planned: 'No planned downloads.',
    };

    return html`
      <div class="p-6">
        <h2 class="text-xl font-semibold mb-4">${tabTitles[this.activeTab as DownloadState]}</h2>

        ${this.loading
          ? html`
              <div class="flex justify-center items-center py-8">
                <span class="loading loading-spinner loading-lg"></span>
              </div>
            `
          : this.error
            ? html`
                <div class="alert alert-error">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    class="stroke-current shrink-0 h-6 w-6"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  <span>Error: ${this.error}</span>
                </div>
              `
            : this.downloadItems.length === 0
              ? html`
                  <div class="text-base-content/70">
                    <p>${emptyMessages[this.activeTab as DownloadState]}</p>
                  </div>
                `
              : html` <div class="space-y-4">${this.downloadItems.map((item) => this.renderDownloadItem(item))}</div> `}
      </div>
    `;
  }

  render() {
    return html`
      <div class="flex flex-col h-screen">
        <app-navbar .activePage=${this.downloaderId}></app-navbar>

        <div class="flex flex-row flex-grow overflow-hidden">
          <div class="flex-1 flex flex-col overflow-hidden">
            <!-- Tabs -->
            <div class="bg-base-100 border-b border-base-300">
              <div role="tablist" class="tabs tabs-box">
                ${this.tabs.map(
                  (tab) => html`
                    <button
                      role="tab"
                      class="tab ${this.activeTab === tab.id ? 'tab-active' : ''}"
                      @click=${() => this.handleTabChange(tab.id)}
                    >
                      ${tab.label}
                    </button>
                  `,
                )}
              </div>
            </div>

            <!-- Tab Content -->
            <div class="flex-1 overflow-y-auto bg-base-200">${this.renderTabContent()}</div>
          </div>
        </div>
      </div>
    `;
  }
}
