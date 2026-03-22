import { Component, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { HttpClientModule } from '@angular/common/http';
import { TrafficListComponent } from './features/traffic-list/traffic-list.component';
import { DetailComponent }      from './features/detail/detail.component';
import { ExchangeStore }        from './core/exchange.store';
import { ProxyApiService }      from './core/proxy-api.service';
import { WsService }            from './core/ws.service';

@Component({
  selector: 'gp-root',
  standalone: true,
  imports: [CommonModule, HttpClientModule, TrafficListComponent, DetailComponent],
  template: `
    <div class="shell">
      <header class="titlebar">
        <div class="titlebar-left">
          <span class="logo">
            <svg viewBox="0 0 20 20" fill="none" width="16" height="16">
              <circle cx="10" cy="10" r="8" stroke="var(--accent)" stroke-width="1.5"/>
              <path d="M6 10l3 3 5-5" stroke="var(--accent)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
            Mitm
          </span>
          <span class="build-tag">dev</span>
        </div>

        <div class="titlebar-center">
          <span class="proxy-addr">
            <svg viewBox="0 0 14 14" fill="none" width="11" height="11">
              <circle cx="7" cy="7" r="5.5" stroke="currentColor" stroke-width="1.2"/>
              <path d="M7 4v3l2 1" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>
            </svg>
            127.0.0.1:8080
          </span>
        </div>

        <div class="titlebar-right">
          <a class="ca-link" [href]="caUrl" download="mitm-ca.crt">Download CA cert</a>
        </div>
      </header>

      <div class="main-split" [class.detail-open]="!!store.selectedUuid()">
        <div class="pane-list">
          <gp-traffic-list />
        </div>
        <div class="pane-detail" *ngIf="store.selectedUuid()">
          <gp-detail />
        </div>
      </div>
    </div>
  `,
  styles: [`
    :host { display: block; height: 100vh; }
    .shell { display: flex; flex-direction: column; height: 100vh; background: var(--bg-1); color: var(--text); }

    .titlebar {
      display: flex; align-items: center; padding: 0 14px; height: 38px;
      background: var(--bg-titlebar); border-bottom: 1px solid var(--border);
      flex-shrink: 0; user-select: none; gap: 0;
    }
    .titlebar-left, .titlebar-center, .titlebar-right {
      display: flex; align-items: center; gap: 8px; flex: 1;
    }
    .titlebar-center { justify-content: center; }
    .titlebar-right  { justify-content: flex-end; }

    .logo {
      display: flex; align-items: center; gap: 7px; font-size: 13px; font-weight: 700;
      color: var(--text); font-family: var(--font-display); letter-spacing: -0.01em;
    }
    .build-tag {
      font-size: 10px; padding: 1px 5px; border-radius: 3px;
      background: rgba(34,197,94,0.12); color: var(--accent);
      font-family: var(--font-mono); border: 1px solid rgba(34,197,94,0.2);
    }
    .proxy-addr {
      display: flex; align-items: center; gap: 5px;
      font-size: 11px; font-family: var(--font-mono); color: var(--text-muted);
    }
    .ca-link {
      font-size: 11px; color: var(--text-muted); text-decoration: none;
      border: 1px solid var(--border); padding: 3px 9px; border-radius: 4px;
      transition: all 0.12s;
    }
    .ca-link:hover { color: var(--accent); border-color: var(--accent); }

    .main-split {
      flex: 1; display: grid; grid-template-columns: 1fr;
      overflow: hidden; transition: grid-template-columns 0.18s ease;
    }
    .main-split.detail-open {
      grid-template-columns: minmax(360px, 1fr) minmax(400px, 1fr);
    }
    .pane-list {
      overflow: hidden; display: flex; flex-direction: column;
      border-right: 1px solid var(--border);
    }
    .pane-detail { overflow: hidden; display: flex; flex-direction: column; }
  `],
})
export class AppComponent {
  readonly store = inject(ExchangeStore);
  readonly caUrl = inject(ProxyApiService).caCertUrl();
  private readonly _ws = inject(WsService);
}
