import {
  Component, inject, ChangeDetectionStrategy, signal,
  ViewChild, ElementRef, effect,
} from '@angular/core';
import { CommonModule, DecimalPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ExchangeStore } from '../../core/exchange.store';
import { ExchangeSummary } from '../../core/models';
import {
  StatusClassPipe, MethodClassPipe, BytesPipe, DurationPipe, ShortTimePipe,
} from '../../shared/pipes/format.pipes';

@Component({
  selector: 'gp-traffic-list',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    CommonModule, FormsModule, DecimalPipe,
    StatusClassPipe, MethodClassPipe, BytesPipe, DurationPipe, ShortTimePipe,
  ],
  template: `
    <!-- ── Filter bar ──────────────────────────────────────────────────── -->
    <div class="filter-bar">
      <div class="search-wrap">
        <svg class="search-icon" viewBox="0 0 16 16" fill="none">
          <circle cx="6.5" cy="6.5" r="4" stroke="currentColor" stroke-width="1.2"/>
          <path d="M10 10l3 3" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/>
        </svg>
        <input
          class="search-input"
          placeholder="Filter URL, host, path…"
          [(ngModel)]="searchValue"
          (ngModelChange)="onSearch($event)"
          spellcheck="false"
        />
        <button class="clear-x" *ngIf="searchValue" (click)="clearSearch()">✕</button>
      </div>

      <div class="filter-pills">
        <button
          *ngFor="let m of methods"
          class="pill" [class.pill-active]="activeMethod() === m"
          (click)="toggleMethod(m)"
        >{{ m }}</button>

        <span class="pill-sep"></span>

        <button
          *ngFor="let s of statusFilters"
          class="pill" [class.pill-active]="activeStatus() === s.key"
          [ngClass]="'pill-' + s.cls"
          (click)="toggleStatus(s.key)"
        >{{ s.label }}</button>
      </div>

      <div class="toolbar-end">
        <span class="count-chip">{{ store.total() | number }}</span>

        <button
          class="tool-btn" [class.tool-btn-amber]="store.paused()"
          (click)="store.togglePause()"
          [title]="store.paused() ? 'Resume live stream' : 'Pause live stream'"
        >
          <!-- Pause icon -->
          <svg *ngIf="!store.paused()" viewBox="0 0 14 14" fill="currentColor" width="13" height="13">
            <rect x="2" y="1.5" width="3.5" height="11" rx="1"/>
            <rect x="8.5" y="1.5" width="3.5" height="11" rx="1"/>
          </svg>
          <!-- Play icon -->
          <svg *ngIf="store.paused()" viewBox="0 0 14 14" fill="currentColor" width="13" height="13">
            <path d="M3 1.5l9 5.5-9 5.5V1.5z"/>
          </svg>
        </button>

        <button class="tool-btn" title="Clear all" (click)="store.clear()">
          <svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.2" width="13" height="13">
            <path d="M2 3.5h10M5.5 3.5V2.5h3v1M11.5 3.5l-.7 8a1 1 0 01-1 .9H4.2a1 1 0 01-1-.9l-.7-8"
                  stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </button>

        <span class="ws-dot" [class.ws-dot-live]="wsConnected()"></span>
      </div>
    </div>

    <!-- ── Column header ───────────────────────────────────────────────── -->
    <div class="col-header">
      <span class="c-id">#</span>
      <span class="c-method">Method</span>
      <span class="c-status">Status</span>
      <span class="c-host">Host</span>
      <span class="c-path">Path</span>
      <span class="c-size">Size</span>
      <span class="c-dur">Time</span>
      <span class="c-ts">Started</span>
    </div>

    <!-- ── Rows ────────────────────────────────────────────────────────── -->
    <div class="rows" #rowsEl>

      <div
        *ngFor="let ex of store.exchanges(); trackBy: trackByUuid"
        class="row"
        [class.row-sel]="store.selectedUuid() === ex.uuid"
        [class.row-err]="!!ex.error"
        (click)="store.select(ex.uuid)"
      >
        <span class="c-id muted">{{ ex.id }}</span>

        <span class="c-method">
          <span class="badge-method" [ngClass]="ex.method | methodClass">{{ ex.method }}</span>
        </span>

        <span class="c-status">
          <span class="badge-status" [ngClass]="ex.status | statusClass">
            {{ ex.status || (ex.error ? 'ERR' : '—') }}
          </span>
        </span>

        <span class="c-host mono muted" [title]="ex.host">{{ ex.host }}</span>
        <span class="c-path mono"       [title]="ex.path">{{ ex.path }}</span>
        <span class="c-size muted">{{ ex.resp_size | bytes }}</span>
        <span class="c-dur" [class.slow]="ex.duration_ms > 1000">{{ ex.duration_ms | duration }}</span>
        <span class="c-ts  muted">{{ ex.started_at | shortTime }}</span>
      </div>

      <!-- Empty state -->
      <div class="empty-state" *ngIf="!store.exchanges().length && !store.loading()">
        <svg viewBox="0 0 48 48" fill="none" width="40" height="40">
          <circle cx="24" cy="24" r="19" stroke="currentColor" stroke-width="1.2" opacity=".2"/>
          <path d="M16 24h16M24 16v16" stroke="currentColor" stroke-width="1.5"
                stroke-linecap="round" opacity=".3"/>
        </svg>
        <p>No traffic captured yet.<br/>
          Point your system proxy to <code>127.0.0.1:8080</code>.</p>
      </div>

    </div>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; height: 100%; overflow: hidden; }

    /* ── Filter bar ────────────────────────────────────────────────────── */
    .filter-bar {
      display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
      padding: 6px 10px; border-bottom: 1px solid var(--border);
      background: var(--bg-2); flex-shrink: 0;
    }

    .search-wrap {
      display: flex; align-items: center; gap: 5px; flex: 0 0 250px;
      background: var(--bg-input); border: 1px solid var(--border);
      border-radius: 5px; padding: 4px 7px;
    }
    .search-wrap:focus-within { border-color: rgba(34,197,94,0.4); }
    .search-icon { width: 12px; height: 12px; color: var(--text-muted); flex-shrink: 0; }
    .search-input {
      border: none; background: transparent; color: var(--text);
      font-family: var(--font-mono); font-size: 12px; outline: none; flex: 1;
      min-width: 0;
    }
    .clear-x {
      background: none; border: none; color: var(--text-muted);
      font-size: 10px; cursor: pointer; padding: 0 2px; line-height: 1;
    }
    .clear-x:hover { color: var(--text); }

    .filter-pills { display: flex; gap: 3px; flex-wrap: wrap; align-items: center; }
    .pill {
      padding: 2px 8px; border-radius: 4px; font-size: 11px; font-family: var(--font-mono);
      border: 1px solid var(--border); background: transparent; color: var(--text-muted);
      cursor: pointer; transition: all 0.1s; white-space: nowrap;
    }
    .pill:hover { border-color: rgba(255,255,255,0.2); color: var(--text); }
    .pill-active { background: rgba(34,197,94,0.12); border-color: rgba(34,197,94,0.35); color: var(--accent); }
    .pill-sep { width: 1px; background: var(--border); height: 14px; margin: 0 1px; }

    /* status pill colours */
    .pill-2xx:hover, .pill-2xx.pill-active { color: #22c55e; border-color: rgba(34,197,94,0.4); background: rgba(34,197,94,0.08); }
    .pill-3xx:hover, .pill-3xx.pill-active { color: #fbbf24; border-color: rgba(251,191,36,0.4); background: rgba(251,191,36,0.08); }
    .pill-4xx:hover, .pill-4xx.pill-active { color: #f97316; border-color: rgba(249,115,22,0.4); background: rgba(249,115,22,0.08); }
    .pill-5xx:hover, .pill-5xx.pill-active { color: #f87171; border-color: rgba(248,113,113,0.4); background: rgba(248,113,113,0.08); }

    .toolbar-end { display: flex; align-items: center; gap: 7px; margin-left: auto; }
    .count-chip {
      font-size: 11px; font-family: var(--font-mono); color: var(--text-muted);
      background: var(--bg-3); padding: 2px 7px; border-radius: 4px;
    }
    .tool-btn {
      display: flex; align-items: center; background: none;
      border: 1px solid var(--border); border-radius: 4px;
      color: var(--text-muted); padding: 3px 5px; cursor: pointer; transition: all 0.1s;
    }
    .tool-btn:hover { color: var(--text); border-color: rgba(255,255,255,0.2); }
    .tool-btn-amber { color: var(--amber); border-color: rgba(251,191,36,0.4); }

    .ws-dot {
      width: 7px; height: 7px; border-radius: 50%; background: var(--text-muted);
      transition: background 0.4s;
    }
    .ws-dot-live { background: var(--green); box-shadow: 0 0 6px rgba(34,197,94,0.6); }

    /* ── Column header ─────────────────────────────────────────────────── */
    .col-header {
      display: grid; grid-template-columns: var(--list-cols);
      padding: 4px 10px; font-size: 10px; font-weight: 600;
      text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-muted);
      border-bottom: 1px solid var(--border); background: var(--bg-2); flex-shrink: 0;
    }

    /* ── Rows container ────────────────────────────────────────────────── */
    .rows { flex: 1; overflow-y: auto; }

    .row {
      display: grid; grid-template-columns: var(--list-cols);
      padding: 0 10px; min-height: 27px; align-items: center;
      border-bottom: 1px solid var(--border-subtle);
      cursor: pointer; transition: background 0.07s; font-size: 12px;
    }
    .row:hover   { background: var(--bg-hover); }
    .row.row-sel { background: var(--bg-selected) !important; }
    .row.row-err { border-left: 2px solid var(--red); padding-left: 8px; }

    /* method badges */
    .badge-method {
      font-size: 10px; font-weight: 700; font-family: var(--font-mono);
      padding: 1px 5px; border-radius: 3px; letter-spacing: 0.02em;
    }
    .method-get    { background: rgba(34,197,94,0.14);  color: #22c55e; }
    .method-post   { background: rgba(59,130,246,0.14); color: #60a5fa; }
    .method-put    { background: rgba(251,191,36,0.14); color: #fbbf24; }
    .method-patch  { background: rgba(168,85,247,0.14); color: #a855f7; }
    .method-delete { background: rgba(239,68,68,0.14);  color: #f87171; }
    .method-head   { background: rgba(20,184,166,0.14); color: #2dd4bf; }
    .method-options{ background: rgba(148,163,184,0.08); color: #94a3b8; }

    /* status badges */
    .badge-status { font-size: 11px; font-family: var(--font-mono); font-weight: 600; }
    .status-2xx { color: #22c55e; }
    .status-3xx { color: #fbbf24; }
    .status-4xx { color: #f97316; }
    .status-5xx { color: #f87171; }
    .status-1xx, .status-0 { color: var(--text-muted); }

    /* columns */
    .c-id, .c-method, .c-status, .c-host, .c-path, .c-size, .c-dur, .c-ts { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .c-host { color: var(--text-muted); }
    .c-path { color: var(--text); }
    .mono   { font-family: var(--font-mono); font-size: 11px; }
    .muted  { color: var(--text-muted); }
    .slow   { color: var(--amber); }

    /* empty state */
    .empty-state {
      display: flex; flex-direction: column; align-items: center; justify-content: center;
      gap: 12px; padding: 60px 20px; color: var(--text-muted); text-align: center;
    }
    .empty-state p { font-size: 13px; line-height: 1.7; }
    .empty-state code {
      font-family: var(--font-mono); font-size: 12px;
      background: var(--bg-3); padding: 1px 5px; border-radius: 3px; color: var(--accent);
    }
  `],
})
export class TrafficListComponent {
  readonly store = inject(ExchangeStore);

  @ViewChild('rowsEl') rowsEl!: ElementRef<HTMLDivElement>;

  searchValue  = '';
  activeMethod = signal('');
  activeStatus = signal('');
  wsConnected  = signal(false);

  readonly methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'];
  readonly statusFilters = [
    { key: '2xx', label: '2xx', cls: '2xx' },
    { key: '3xx', label: '3xx', cls: '3xx' },
    { key: '4xx', label: '4xx', cls: '4xx' },
    { key: '5xx', label: '5xx', cls: '5xx' },
  ];

  constructor() {
    this.store.connected$.subscribe(c => this.wsConnected.set(c));

    // Auto-scroll to top when new exchanges arrive (only if near top).
    effect(() => {
      // Depend on the exchanges signal.
      const _ = this.store.exchanges();
      const el = this.rowsEl?.nativeElement;
      if (el && el.scrollTop < 60) el.scrollTop = 0;
    });
  }

  onSearch(value: string): void {
    this.store.setFilter({ search: value || undefined });
  }

  clearSearch(): void {
    this.searchValue = '';
    this.store.setFilter({ search: undefined });
  }

  toggleMethod(m: string): void {
    const next = this.activeMethod() === m ? '' : m;
    this.activeMethod.set(next);
    this.store.setFilter({ method: next || undefined });
  }

  toggleStatus(key: string): void {
    const next = this.activeStatus() === key ? '' : key;
    this.activeStatus.set(next);
    const ranges: Record<string, [number, number]> = {
      '2xx': [200, 299], '3xx': [300, 399],
      '4xx': [400, 499], '5xx': [500, 599],
    };
    const range = ranges[next];
    this.store.setFilter({
      status_min: range?.[0],
      status_max: range?.[1],
    });
  }

  trackByUuid(_: number, ex: ExchangeSummary): string {
    return ex.uuid;
  }
}
