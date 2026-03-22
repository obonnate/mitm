import {
  Component, inject, ChangeDetectionStrategy, signal, effect, OnInit,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { ExchangeStore } from '../../core/exchange.store';
import { ProxyApiService } from '../../core/proxy-api.service';
import { ExchangeDetail } from '../../core/models';
import {
  StatusClassPipe, MethodClassPipe, DurationPipe, BytesPipe,
} from '../../shared/pipes/format.pipes';
import { HeadersPaneComponent }  from './headers/headers-pane.component';
import { BodyPaneComponent }     from './body/body-pane.component';
import { TimelinePaneComponent } from './timeline/timeline-pane.component';
import { TlsPaneComponent }      from './tls/tls-pane.component';
import { ReplayPaneComponent }   from './replay/replay-pane.component';

type Tab = 'headers' | 'request' | 'response' | 'timeline' | 'tls' | 'replay';

@Component({
  selector: 'gp-detail',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    CommonModule,
    StatusClassPipe, MethodClassPipe, DurationPipe, BytesPipe,
    HeadersPaneComponent, BodyPaneComponent, TimelinePaneComponent,
    TlsPaneComponent, ReplayPaneComponent,
  ],
  template: `
    <!-- Empty state -->
    <div class="detail-empty" *ngIf="!store.selectedUuid()">
      <svg viewBox="0 0 48 48" fill="none" width="36" height="36">
        <rect x="4" y="8" width="40" height="32" rx="4"
              stroke="currentColor" stroke-width="1.4" opacity=".25"/>
        <path d="M4 16h40" stroke="currentColor" stroke-width="1.4" opacity=".25"/>
        <path d="M12 24h10M12 30h16" stroke="currentColor" stroke-width="1.4"
              stroke-linecap="round" opacity=".25"/>
      </svg>
      <p>Select a request to inspect</p>
    </div>

    <ng-container *ngIf="store.selectedUuid()">

      <!-- Skeleton while loading -->
      <ng-container *ngIf="!detail()">
        <div class="skel-topbar">
          <div class="skel skel-badge"></div>
          <div class="skel skel-url"></div>
        </div>
        <div class="skel-body">
          <div class="skel skel-line w70"></div>
          <div class="skel skel-line w45"></div>
          <div class="skel skel-line w80"></div>
        </div>
      </ng-container>

      <!-- Loaded -->
      <ng-container *ngIf="detail() as d">

        <!-- Top bar: method · URL · status · TLS -->
        <div class="detail-topbar">
          <span class="method-badge" [class]="d.method | methodClass">{{ d.method }}</span>
          <span class="detail-url" [title]="d.host + d.path">
            <span class="url-host">{{ d.host }}</span><span class="url-path">{{ d.path }}</span>
          </span>
          <span class="status-badge" [class]="d.status | statusClass">{{ d.status }}</span>
          <span class="tls-pill" *ngIf="d.tls">{{ d.tls_info?.version || 'TLS' }}</span>
        </div>

        <!-- Meta strip -->
        <div class="meta-strip">
          <span class="meta-item mono">#{{ d.id }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-item">{{ d.duration_ms | duration }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-item">↑ {{ d.req_size | bytes }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-item">↓ {{ d.resp_size | bytes }}</span>
          <span class="meta-dot">·</span>
          <span class="meta-item proto">{{ d.protocol }}</span>
        </div>

        <!-- Tab bar -->
        <div class="tabs">
          <button
            *ngFor="let t of tabs"
            class="tab"
            [class.active]="activeTab() === t.id"
            [hidden]="t.id === 'tls' && !d.tls"
            (click)="activeTab.set(t.id)"
          >
            {{ t.label }}
            <span class="tab-badge" *ngIf="t.id === 'headers'">
              {{ headerCount(d) }}
            </span>
          </button>
        </div>

        <!-- Pane area -->
        <div class="pane-scroll">
          <gp-headers-pane  *ngIf="activeTab() === 'headers'"  [detail]="d" />
          <gp-body-pane     *ngIf="activeTab() === 'request'"  [body]="d.req_body"  [headers]="d.req_headers" />
          <gp-body-pane     *ngIf="activeTab() === 'response'" [body]="d.resp_body" [headers]="d.resp_headers" />
          <gp-timeline-pane *ngIf="activeTab() === 'timeline'" [timing]="d.timing" />
          <gp-tls-pane      *ngIf="activeTab() === 'tls'"      [tlsInfo]="d.tls_info!" [detail]="d" />
          <gp-replay-pane   *ngIf="activeTab() === 'replay'"   [detail]="d" />
        </div>

      </ng-container>
    </ng-container>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; height: 100%; overflow: hidden; background: var(--bg-1); }

    /* ── Empty ───────────────────────────────────────────────────────────── */
    .detail-empty {
      flex: 1; display: flex; flex-direction: column; align-items: center;
      justify-content: center; gap: 10px; color: var(--text-muted); font-size: 13px;
    }

    /* ── Skeleton ────────────────────────────────────────────────────────── */
    .skel {
      background: var(--bg-3); border-radius: 4px;
      animation: pulse 1.4s ease-in-out infinite;
    }
    .skel-topbar { display: flex; gap: 8px; padding: 10px 12px; align-items: center; }
    .skel-badge  { width: 42px; height: 20px; border-radius: 3px; flex-shrink: 0; }
    .skel-url    { height: 14px; flex: 1; }
    .skel-body   { padding: 16px 12px; display: flex; flex-direction: column; gap: 10px; }
    .skel-line   { height: 11px; border-radius: 3px; }
    .w45 { width: 45%; } .w70 { width: 70%; } .w80 { width: 80%; }

    /* ── Top bar ─────────────────────────────────────────────────────────── */
    .detail-topbar {
      display: flex; align-items: center; gap: 8px; padding: 8px 12px;
      border-bottom: 1px solid var(--border); background: var(--bg-2);
      flex-shrink: 0; min-height: 40px;
    }
    .detail-url {
      flex: 1; font-family: var(--font-mono); font-size: 12px;
      overflow: hidden; white-space: nowrap; text-overflow: ellipsis;
    }
    .url-host { color: var(--text-muted); }
    .url-path  { color: var(--text); }

    /* method badges */
    .method-badge {
      font-size: 10px; font-weight: 700; font-family: var(--font-mono);
      padding: 2px 6px; border-radius: 3px; flex-shrink: 0; letter-spacing: 0.03em;
    }
    .method-get    { background: rgba(34,197,94,0.15);  color: #22c55e; }
    .method-post   { background: rgba(59,130,246,0.15); color: #60a5fa; }
    .method-put    { background: rgba(251,191,36,0.15); color: #fbbf24; }
    .method-patch  { background: rgba(168,85,247,0.15); color: #a855f7; }
    .method-delete { background: rgba(239,68,68,0.15);  color: #f87171; }
    .method-head   { background: rgba(20,184,166,0.15); color: #2dd4bf; }
    .method-options{ background: rgba(148,163,184,0.1); color: #94a3b8; }

    /* status */
    .status-badge { font-size: 12px; font-family: var(--font-mono); font-weight: 600; flex-shrink: 0; }
    .status-2xx { color: #22c55e; } .status-3xx { color: #fbbf24; }
    .status-4xx { color: #f97316; } .status-5xx { color: #f87171; }
    .status-1xx,.status-0 { color: var(--text-muted); }

    .tls-pill {
      font-size: 10px; padding: 2px 6px; border-radius: 3px; flex-shrink: 0;
      background: rgba(34,197,94,0.1); color: #22c55e; border: 1px solid rgba(34,197,94,0.2);
    }

    /* ── Meta strip ──────────────────────────────────────────────────────── */
    .meta-strip {
      display: flex; align-items: center; gap: 6px; padding: 4px 12px;
      font-size: 11px; color: var(--text-muted); background: var(--bg-2);
      border-bottom: 1px solid var(--border); flex-shrink: 0;
    }
    .meta-dot  { color: var(--border); }
    .meta-item { font-family: var(--font-mono); }
    .proto { color: var(--text-dim); }

    /* ── Tabs ────────────────────────────────────────────────────────────── */
    .tabs {
      display: flex; border-bottom: 1px solid var(--border); background: var(--bg-2);
      padding: 0 6px; flex-shrink: 0; overflow-x: auto;
    }
    .tab {
      padding: 8px 11px; font-size: 12px; color: var(--text-muted);
      background: none; border: none; border-bottom: 2px solid transparent;
      cursor: pointer; white-space: nowrap; transition: all 0.12s;
      display: flex; align-items: center; gap: 5px;
    }
    .tab:hover  { color: var(--text); }
    .tab.active { color: var(--accent); border-bottom-color: var(--accent); }
    .tab-badge {
      font-size: 10px; background: var(--bg-3); color: var(--text-muted);
      padding: 1px 5px; border-radius: 8px; font-family: var(--font-mono);
    }

    /* ── Pane scroll area ────────────────────────────────────────────────── */
    .pane-scroll { flex: 1; overflow-y: auto; }
  `],
})
export class DetailComponent {
  readonly store = inject(ExchangeStore);
  private readonly api = inject(ProxyApiService);

  readonly detail    = signal<ExchangeDetail | null>(null);
  readonly activeTab = signal<Tab>('headers');

  readonly tabs: { id: Tab; label: string }[] = [
    { id: 'headers',  label: 'Headers'  },
    { id: 'request',  label: 'Request'  },
    { id: 'response', label: 'Response' },
    { id: 'timeline', label: 'Timeline' },
    { id: 'tls',      label: 'TLS'      },
    { id: 'replay',   label: 'Replay'   },
  ];

  constructor() {
    // Re-fetch detail whenever selected UUID changes.
    effect(() => {
      const uuid = this.store.selectedUuid();
      if (!uuid) {
        this.detail.set(null);
        return;
      }
      this.detail.set(null);   // show skeleton immediately
      this.api.get(uuid).subscribe(d => this.detail.set(d));
    }, { allowSignalWrites: true });
  }

  headerCount(d: ExchangeDetail): number {
    return Object.keys(d.req_headers  ?? {}).length
         + Object.keys(d.resp_headers ?? {}).length;
  }
}
