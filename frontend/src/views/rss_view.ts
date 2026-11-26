import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import "../components/navbar.ts";
import globalStyles from "/src/index.css?inline";
import {
  deleteSearch,
  errorMessage,
  fetchIndexers,
  fetchSearches,
  registerSearch,
  type RSSSearch,
} from "../utils/api.ts";
import { renderError } from "../components/error_alert.ts";

@customElement("rss-view")
export class RssView extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @state() private searches: RSSSearch[] = [];
  @state() private indexers: string[] = [];
  @state() private loading = true;
  @state() private error: string | null = null;
  @state() private actionError: string | null = null;
  @state() private adding = false;
  @state() private deleting: Set<number> = new Set();

  private indexer = "";
  private action: RSSSearch["action"] = "download";

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.error = null;
    try {
      const [searches, indexers] = await Promise.all([fetchSearches(), fetchIndexers()]);
      this.searches = searches;
      this.indexers = indexers;
      if (!this.indexer || !indexers.includes(this.indexer)) this.indexer = indexers[0] ?? "";
    } catch (err) {
      this.error = errorMessage(err);
    } finally {
      this.loading = false;
    }
  }

  private retry = () => {
    this.loading = true;
    void this.load();
  };

  private dismissActionError = () => {
    this.actionError = null;
  };

  private async handleAdd(e: Event) {
    e.preventDefault();
    if (this.adding) return;
    const form = e.currentTarget as HTMLFormElement;
    const input = form.elements.namedItem("text") as HTMLInputElement;
    const text = input.value.trim();
    if (!text || !this.indexer) return;
    this.adding = true;
    this.actionError = null;
    try {
      await registerSearch(this.indexer, text, this.action);
      input.value = "";
      this.searches = await fetchSearches();
    } catch (err) {
      this.actionError = `Failed to add search: ${errorMessage(err)}`;
    } finally {
      this.adding = false;
    }
  }

  private async handleDelete(search: RSSSearch) {
    if (this.deleting.has(search.id)) return;
    if (!confirm(`Delete the RSS search "${search.text}" on ${search.indexer}?`)) return;
    this.deleting = new Set(this.deleting).add(search.id);
    this.actionError = null;
    try {
      await deleteSearch(search.id);
      this.searches = await fetchSearches();
    } catch (err) {
      this.actionError = `Failed to delete search: ${errorMessage(err)}`;
    } finally {
      const rest = new Set(this.deleting);
      rest.delete(search.id);
      this.deleting = rest;
    }
  }

  private renderRow(s: RSSSearch) {
    const busy = this.deleting.has(s.id);
    return html`
      <tr>
        <td>${s.indexer}</td>
        <td class="font-mono break-all">${s.text}</td>
        <td>
          <span class="badge badge-sm ${s.action === "download" ? "badge-info" : "badge-ghost"}"
            >${s.action === "download" ? "Download" : "Notify"}</span
          >
        </td>
        <td>
          ${
            s.res_id
              ? html`<span class="badge badge-success badge-sm">Matched</span>
                  <span class="ml-1">${s.title ?? ""}</span>`
              : "Waiting"
          }
        </td>
        <td>${new Date(s.created_at).toLocaleString()}</td>
        <td>
          <button
            class="btn btn-xs btn-ghost text-error"
            aria-label="Delete search ${s.text}"
            ?disabled=${busy}
            @click=${() => this.handleDelete(s)}
          >
            Delete
          </button>
        </td>
      </tr>
    `;
  }

  private renderBody() {
    if (this.loading) {
      return html`<div class="flex justify-center py-8" role="status" aria-live="polite">
        <span class="loading loading-spinner loading-lg" aria-hidden="true"></span>
        <span class="sr-only">Loading...</span>
      </div>`;
    }
    if (this.error) {
      return renderError(`Failed to load searches: ${this.error}`, this.retry);
    }
    if (this.searches.length === 0) {
      return html`<p class="text-base-content/70" aria-live="polite">No RSS searches yet.</p>`;
    }
    return html`
      <div class="overflow-x-auto">
        <table class="table table-sm">
          <thead>
            <tr>
              <th>Indexer</th>
              <th>Text</th>
              <th>Action</th>
              <th>Status</th>
              <th>Created</th>
              <th><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            ${this.searches.map((s) => this.renderRow(s))}
          </tbody>
        </table>
      </div>
    `;
  }

  render() {
    return html`
      <div class="flex flex-col h-screen">
        <app-navbar activePage="rss"></app-navbar>
        <div
          class="p-2 bg-base-200 text-base-content flex flex-col items-center overflow-y-auto grow"
        >
          <div
            class="bg-base-100 p-3 sm:p-6 rounded-2xl shadow-xl w-full max-w-6xl border border-base-300"
          >
            <h2 class="text-xl font-semibold mb-4">RSS searches</h2>
            <form class="join w-full mb-4" @submit=${this.handleAdd}>
              <select
                class="select select-bordered join-item"
                aria-label="Indexer"
                @change=${(e: Event) => (this.indexer = (e.target as HTMLSelectElement).value)}
              >
                ${this.indexers.map(
                  (i) => html`<option value=${i} ?selected=${i === this.indexer}>${i}</option>`,
                )}
              </select>
              <input
                name="text"
                class="input input-bordered join-item w-full"
                placeholder="Text to match"
                aria-label="Search text"
                required
              />
              <select
                class="select select-bordered join-item"
                aria-label="Action"
                @change=${(e: Event) =>
                  (this.action = (e.target as HTMLSelectElement).value as RSSSearch["action"])}
              >
                <option value="download">Download</option>
                <option value="notification">Notify</option>
              </select>
              <button class="btn btn-primary join-item" type="submit" ?disabled=${this.adding}>
                Add
              </button>
            </form>
            ${
              this.actionError
                ? html`<div class="mb-4">
                    ${renderError(this.actionError, undefined, this.dismissActionError)}
                  </div>`
                : ""
            }
            ${this.renderBody()}
          </div>
        </div>
      </div>
    `;
  }
}
