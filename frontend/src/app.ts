import 'urlpattern-polyfill';
import { html, LitElement } from 'lit';
import { customElement } from 'lit/decorators.js';

import './router.ts';

@customElement('app-root')
export class App extends LitElement {
  render() {
    return html` <app-router></app-router> `;
  }
}
