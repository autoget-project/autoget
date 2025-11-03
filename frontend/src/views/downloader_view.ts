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

  @property({ type: String })
  public activeTab: string = 'downloading';

  @state()
  private downloadItems: DownloadItem[] = [];

  @state()
  private loading: boolean = true;

  @state()
  private initialLoad: boolean = true;

  @state()
  private error: string | null = null;

  @state()
  private refreshInterval: number = 20; // Default to 20 seconds

  @state()
  private userHints: Map<string, string> = new Map(); // Store user hints per download ID

  private refreshTimer: number | null = null;

  private readonly tabs = [
    { id: 'downloading', label: 'Downloading' },
    { id: 'seeding', label: 'Seeding' },
    { id: 'stopped', label: 'Stopped' },
    { id: 'planned', label: 'Planned' },
    { id: 'failed', label: 'Failed' },
  ];

  protected async firstUpdated() {
    await this.loadDownloadItems();
    this.startRefreshTimer();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.stopRefreshTimer();
  }

  protected async updated(changedProperties: Map<string, unknown>) {
    if (changedProperties.has('activeTab') || changedProperties.has('downloaderId')) {
      // Update URL when activeTab changes
      if (changedProperties.has('activeTab') && this.downloaderId) {
        const newUrl = `/downloaders/${this.downloaderId}/${this.activeTab}`;
        history.pushState(null, '', newUrl);
      }
      await this.loadDownloadItems();
    }
  }

  private async loadDownloadItems() {
    if (!this.downloaderId) return;

    this.error = null;

    try {
      const newItems = await fetchDownloaderItems(this.downloaderId, this.activeTab as DownloadState);

      // Only update if data has actually changed or if it's the initial load
      if (this.initialLoad || !this.arraysEqual(this.downloadItems, newItems)) {
        this.downloadItems = newItems;
        this.initialLoad = false;
      }
    } catch (err) {
      this.error = err instanceof Error ? err.message : 'Failed to load download items';
      // Only clear items on error if there are no items yet
      if (this.downloadItems.length === 0) {
        this.downloadItems = [];
      }
      this.initialLoad = false;
    } finally {
      this.loading = false;
    }
  }

  private arraysEqual(a: DownloadItem[], b: DownloadItem[]): boolean {
    if (a.length !== b.length) return false;

    // Create a map for quick lookup by ID
    const aMap = new Map(a.map((item) => [item.ID, item]));
    const bMap = new Map(b.map((item) => [item.ID, item]));

    // Check if all IDs match
    if (aMap.size !== bMap.size) return false;

    // Check each item's key properties
    for (const [id, aItem] of aMap) {
      const bItem = bMap.get(id);
      if (!bItem) return false;

      // Compare key properties that affect display
      if (
        aItem.DownloadProgress !== bItem.DownloadProgress ||
        aItem.MoveState !== bItem.MoveState ||
        aItem.OrganizeState !== bItem.OrganizeState ||
        aItem.State !== bItem.State ||
        aItem.UpdatedAt !== bItem.UpdatedAt ||
        JSON.stringify(aItem.OrganizePlans) !== JSON.stringify(bItem.OrganizePlans)
      ) {
        return false;
      }
    }

    return true;
  }

  private handleTabChange(tabId: string) {
    if (this.activeTab !== tabId) {
      this.activeTab = tabId;
      this.initialLoad = true; // Reset for new tab
      this.loading = true;
    }
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
      case 3: // CreatePlanFailed
        return { label: 'Create Failed', color: 'error' };
      case 4: // ExecutePlanFailed
        return { label: 'Execute Failed', color: 'error' };
      default:
        return { label: 'Unknown', color: 'neutral' };
    }
  }

  private async handleOrganizeAction(downloadId: string, action: OrganizeAction, userHint?: string) {
    try {
      const success = await organizeDownload(downloadId, action, userHint);
      if (success) {
        // Clear the user hint for this download if the action was successful
        if (userHint) {
          this.userHints.delete(downloadId);
        }
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

  private handleUserHintChange(downloadId: string, event: Event) {
    const input = event.target as HTMLTextAreaElement;
    this.userHints.set(downloadId, input.value);
    this.requestUpdate();
  }

  private async handleReplanWithHint(downloadId: string) {
    const userHint = this.userHints.get(downloadId) || '';
    await this.handleOrganizeAction(downloadId, 're_plan', userHint);
  }

  private startRefreshTimer() {
    this.stopRefreshTimer(); // Clear any existing timer
    this.refreshTimer = setInterval(() => {
      this.loadDownloadItems();
    }, this.refreshInterval * 1000);
  }

  private stopRefreshTimer() {
    if (this.refreshTimer !== null) {
      clearInterval(this.refreshTimer);
      this.refreshTimer = null;
    }
  }

  private handleRefreshIntervalChange(event: Event) {
    const select = event.target as HTMLSelectElement;
    this.refreshInterval = parseInt(select.value);
    this.startRefreshTimer(); // Restart timer with new interval
  }

  private renderOrganizePlans(organizePlans: PlanResponse | null, downloadId: string) {
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

    const currentUserHint = this.userHints.get(downloadId) || '';

    return html`
      <div class="mt-4">
        <details tabindex="0" class="collapse collapse-arrow bg-base-200">
          <summary class="collapse-title text-lg font-medium">
            Organize Plan (${organizePlans.plan.length} items)
          </summary>
          <div class="collapse-content">
            <!-- Feedback Section - Only show in planned tab -->
            ${this.activeTab === 'planned' ? html`
              <div class="pb-4">
                <h4 class="text-sm font-medium mb-2">Provide feedback for re-creating plan:</h4>
                <div class="flex gap-2">
                  <input
                    type="text"
                    class="input input-bordered input-sm flex-1"
                    placeholder="E.g., 'Move movie files to /Movies/Action folder', 'Skip subtitle files'"
                    .value=${currentUserHint}
                    @input=${(e: Event) => this.handleUserHintChange(downloadId, e)}
                    @keyup=${(e: KeyboardEvent) => {
                      if (e.key === 'Enter' && currentUserHint.trim()) {
                        this.handleReplanWithHint(downloadId);
                      }
                    }}
                  />
                  <button
                    class="btn btn-sm btn-primary btn-square"
                    @click=${() => this.handleReplanWithHint(downloadId)}
                    ?disabled=${!currentUserHint.trim()}
                    title="Send feedback"
                  >
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      class="h-4 w-4"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"
                      />
                    </svg>
                  </button>
                </div>
              </div>
            ` : ''}

            <div class="border-t border-base-300 pt-4">
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
        </details>
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
            </div>
            <div class="flex items-center gap-4">
              ${this.activeTab === 'downloading'
                ? html`
                    <div
                      class="radial-progress text-primary"
                      style="--value:${item.DownloadProgress / 10};"
                      aria-valuenow=${item.DownloadProgress / 10}
                      role="progressbar"
                    >
                      ${item.DownloadProgress / 10}%
                    </div>
                  `
                : html``}
              <div class="card-actions flex-col gap-2">
                ${this.activeTab === 'planned'
                  ? html`
                      <button
                        class="btn btn-sm btn-success"
                        @click=${() => this.handleOrganizeAction(item.ID, 'accept_plan')}
                      >
                        Accept Plan
                      </button>
                      <button class="btn btn-sm btn-info" @click=${() => this.handleOrganizeAction(item.ID, 're_plan')}>
                        Re-plan
                      </button>
                      <button
                        class="btn btn-sm btn-neutral"
                        @click=${() => this.handleOrganizeAction(item.ID, 'manual_organized')}
                      >
                        Manual Organized
                      </button>
                    `
                  : this.activeTab === 'failed'
                    ? html`
                        <button
                          class="btn btn-sm btn-info"
                          @click=${() => this.handleOrganizeAction(item.ID, 're_plan')}
                        >
                          Re-plan
                        </button>
                        <button
                          class="btn btn-sm btn-neutral"
                          @click=${() => this.handleOrganizeAction(item.ID, 'manual_organized')}
                        >
                          Manual Organized
                        </button>
                      `
                    : html``}
              </div>
            </div>
          </div>
          ${this.renderOrganizePlans(item.OrganizePlans, item.ID)}
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
      failed: 'Failed Downloads',
    };

    const emptyMessages = {
      downloading: 'No active downloads at the moment.',
      seeding: 'No torrents are currently seeding.',
      stopped: 'No stopped downloads.',
      planned: 'No planned downloads.',
      failed: 'No failed downloads.',
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

        <div class="flex flex-row grow overflow-hidden">
          <div class="flex-1 flex flex-col overflow-hidden">
            <!-- Tabs -->
            <div class="bg-base-100 border-b border-base-300">
              <div class="flex items-center justify-between px-4">
                <div role="tablist" class="tabs tabs-border">
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

                <!-- Refresh Dropdown -->
                <div class="flex items-center gap-2">
                  <span class="text-sm text-base-content/70">Refresh:</span>
                  <select
                    class="select select-bordered select-sm"
                    @change=${this.handleRefreshIntervalChange}
                    .value=${String(this.refreshInterval)}
                  >
                    <option value="10">10s</option>
                    <option value="20">20s</option>
                    <option value="30">30s</option>
                    <option value="60">60s</option>
                  </select>
                </div>
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
