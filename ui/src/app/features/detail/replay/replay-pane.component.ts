import {
  Component, Input, ChangeDetectionStrategy, signal, inject,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ProxyApiService } from '../../../core/proxy-api.service';
import { ExchangeDetail, ReplayResponse } from '../../../core/models';
import { StatusClassPipe } from '../../../shared/pipes/format.pipes';

@Component({
  selector: 'gp-replay-pane',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule, FormsModule, StatusClassPipe],
  template: `
    <div class="replay-wrap">
      <!-- Request editor -->
      <div class="field-row">
        <label>Method</label>
        <select class="mono-input small" [(ngModel)]="method">
          <option *ngFor="let m of methods" [value]="m">{{ m }}</option>
        </select>
        <input class="mono-input flex1" [(ngModel)]="url" placeholder="URL" />
      </div>

      <div class="field-row top">
        <label>Headers</label>
        <textarea class="mono-input flex1" rows="5" [(ngModel)]="headersText"
          placeholder="Content-Type: application/json&#10;Authorization: Bearer …"></textarea>
      </div>

      <div class="field-row top">
        <label>Body</label>
        <textarea class="mono-input flex1" rows="7" [(ngModel)]="bodyText"></textarea>
      </div>

      <!-- Actions -->
      <div class="replay-actions">
        <button class="btn-send" (click)="send()" [disabled]="sending()">
          <span *ngIf="!sending()">Send</span>
          <span *ngIf="sending()">Sending…</span>
        </button>
        <button class="btn-secondary" (click)="copyCurl()">Copy as cURL</button>
        <button class="btn-secondary" (click)="copyFetch()">Copy as fetch</button>
      </div>

      <!-- Response -->
      <ng-container *ngIf="response()">
        <div class="resp-bar">
          <span class="status-badge" [class]="response()!.status | statusClass">{{ response()!.status }}</span>
          <span class="resp-label">Replay response</span>
        </div>
        <div class="resp-headers">
          <div *ngFor="let h of respHeaderList()">
            <span class="rh-name">{{ h.name }}:</span>
            <span class="rh-val">{{ h.value }}</span>
          </div>
        </div>
        <pre class="resp-body">{{ prettyBody() }}</pre>
      </ng-container>

      <div class="replay-error" *ngIf="error()">{{ error() }}</div>
    </div>
  `,
  styles: [`
    .replay-wrap { padding: 12px; display: flex; flex-direction: column; gap: 10px; }

    .field-row {
      display: flex; align-items: center; gap: 10px;
    }
    .field-row.top { align-items: flex-start; }
    label {
      font-size: 11px; color: var(--text-muted); width: 60px;
      flex-shrink: 0; padding-top: 5px;
    }
    .mono-input {
      font-family: var(--font-mono); font-size: 12px; padding: 5px 8px;
      background: var(--bg-input); border: 1px solid var(--border);
      border-radius: 5px; color: var(--text); outline: none; resize: vertical;
    }
    .mono-input:focus { border-color: var(--accent); }
    .mono-input.small { width: 80px; flex-shrink: 0; }
    .mono-input.flex1 { flex: 1; }

    .replay-actions { display: flex; gap: 8px; padding-top: 4px; }
    .btn-send {
      padding: 6px 18px; border-radius: 5px; font-size: 12px; font-weight: 500;
      background: var(--accent); color: #000; border: none; cursor: pointer;
      transition: opacity 0.12s;
    }
    .btn-send:disabled { opacity: 0.5; cursor: not-allowed; }
    .btn-secondary {
      padding: 6px 12px; border-radius: 5px; font-size: 12px;
      background: none; border: 1px solid var(--border); color: var(--text-muted);
      cursor: pointer; transition: all 0.12s;
    }
    .btn-secondary:hover { color: var(--text); border-color: var(--text-muted); }

    .resp-bar {
      display: flex; align-items: center; gap: 8px; padding: 8px 0 4px;
      border-top: 1px solid var(--border); margin-top: 4px;
    }
    .resp-label { font-size: 11px; color: var(--text-muted); }
    .status-badge { font-size: 12px; font-family: var(--font-mono); font-weight: 600; }
    .status-2xx { color: #22c55e; } .status-3xx { color: #fbbf24; }
    .status-4xx { color: #f97316; } .status-5xx { color: #f87171; }

    .resp-headers { display: flex; flex-direction: column; gap: 2px; margin-bottom: 8px; }
    .rh-name { font-family: var(--font-mono); font-size: 11px; color: var(--text-muted); margin-right: 6px; }
    .rh-val  { font-family: var(--font-mono); font-size: 11px; color: var(--text); }

    .resp-body {
      font-family: var(--font-mono); font-size: 12px; line-height: 1.6;
      background: var(--bg-2); border: 1px solid var(--border); border-radius: 5px;
      padding: 10px 12px; white-space: pre-wrap; word-break: break-all; margin: 0;
      max-height: 280px; overflow: auto;
    }
    .replay-error {
      font-size: 12px; color: var(--red); padding: 8px 10px;
      background: rgba(239,68,68,0.08); border: 1px solid rgba(239,68,68,0.2); border-radius: 5px;
    }
  `],
})
export class ReplayPaneComponent {
  @Input() set detail(d: ExchangeDetail) {
    this._detail = d;
    this.method = d.method;
    this.url = `${d.tls ? 'https' : 'http'}://${d.host}${d.path}`;
    this.bodyText = d.req_body ?? '';
    this.headersText = Object.entries(d.req_headers ?? {})
      .flatMap(([k, vs]) => vs.map(v => `${k}: ${v}`))
      .join('\n');
    this.response.set(null);
    this.error.set(null);
  }
  private _detail!: ExchangeDetail;
  private readonly api = inject(ProxyApiService);

  method = 'GET';
  url = '';
  headersText = '';
  bodyText = '';

  sending  = signal(false);
  response = signal<ReplayResponse | null>(null);
  error    = signal<string | null>(null);

  readonly methods = ['GET','POST','PUT','PATCH','DELETE','HEAD','OPTIONS'];

  send(): void {
    this.sending.set(true);
    this.error.set(null);
    this.response.set(null);

    const headers: Record<string, string> = {};
    for (const line of this.headersText.split('\n')) {
      const idx = line.indexOf(':');
      if (idx > 0) headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
    }

    this.api.replay(this._detail.uuid, {
      method:  this.method,
      url:     this.url,
      headers,
      body:    this.bodyText || undefined,
    }).subscribe({
      next:  r => { this.response.set(r); this.sending.set(false); },
      error: e => { this.error.set(e.message); this.sending.set(false); },
    });
  }

  respHeaderList(): { name: string; value: string }[] {
    const r = this.response();
    if (!r?.headers) return [];
    return Object.entries(r.headers).flatMap(([k, vs]) => vs.map(v => ({ name: k, value: v })));
  }

  prettyBody(): string {
    const b = this.response()?.body ?? '';
    try { return JSON.stringify(JSON.parse(b), null, 2); } catch { return b; }
  }

  copyCurl(): void {
    const headers = this.headersText.split('\n')
      .filter(l => l.includes(':'))
      .map(l => `-H '${l.trim()}'`)
      .join(' \\\n  ');
    const body = this.bodyText ? `-d '${this.bodyText.replace(/'/g, "'\\''")}' \\\n  ` : '';
    const curl = `curl -X ${this.method} \\\n  ${headers} \\\n  ${body}'${this.url}'`;
    navigator.clipboard?.writeText(curl).catch(() => {});
  }

  copyFetch(): void {
    const headers: Record<string, string> = {};
    for (const line of this.headersText.split('\n')) {
      const idx = line.indexOf(':');
      if (idx > 0) headers[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
    }
    const opts: Record<string, unknown> = { method: this.method, headers };
    if (this.bodyText) opts['body'] = this.bodyText;
    const code = `await fetch('${this.url}', ${JSON.stringify(opts, null, 2)});`;
    navigator.clipboard?.writeText(code).catch(() => {});
  }
}
