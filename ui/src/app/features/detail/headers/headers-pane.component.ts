import { Component, Input, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ExchangeDetail } from '../../../core/models';

@Component({
  selector: 'gp-headers-pane',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule],
  template: `
    <section class="hdr-section">
      <div class="sec-head">Request headers</div>
      <table class="hdr-table">
        <tr *ngFor="let h of reqHeaders()" class="hdr-row" (click)="copy(h.name + ': ' + h.value)">
          <td class="hdr-name">{{ h.name }}</td>
          <td class="hdr-value" [class.sensitive]="isSensitive(h.name)">
            <span *ngIf="!isSensitive(h.name)">{{ h.value }}</span>
            <span *ngIf="isSensitive(h.name)" class="masked" (click)="toggleMask(h.name, $event)">
              {{ masked.has(h.name) ? '••••••••' : h.value }}
            </span>
          </td>
        </tr>
      </table>
    </section>
    <section class="hdr-section">
      <div class="sec-head">Response headers</div>
      <table class="hdr-table">
        <tr *ngFor="let h of respHeaders()" class="hdr-row" (click)="copy(h.name + ': ' + h.value)">
          <td class="hdr-name">{{ h.name }}</td>
          <td class="hdr-value">{{ h.value }}</td>
        </tr>
      </table>
    </section>
  `,
  styles: [`
    .hdr-section { border-bottom: 1px solid var(--border); }
    .sec-head {
      padding: 5px 12px; font-size: 10px; font-weight: 600; letter-spacing: 0.06em;
      text-transform: uppercase; color: var(--text-muted); background: var(--bg-2);
      border-bottom: 1px solid var(--border);
    }
    .hdr-table { width: 100%; border-collapse: collapse; }
    .hdr-row { cursor: pointer; }
    .hdr-row:hover td { background: var(--bg-hover); }
    .hdr-name {
      padding: 4px 12px; font-family: var(--font-mono); font-size: 12px;
      color: var(--text-muted); width: 36%; vertical-align: top;
      white-space: nowrap;
    }
    .hdr-value {
      padding: 4px 12px 4px 0; font-family: var(--font-mono); font-size: 12px;
      color: var(--text); word-break: break-all;
    }
    .sensitive .masked { color: var(--amber); cursor: pointer; }
    .sensitive .masked:hover { text-decoration: underline dotted; }
  `],
})
export class HeadersPaneComponent {
  @Input() detail!: ExchangeDetail;

  masked = new Set<string>();

  private readonly sensitiveNames = new Set([
    'authorization', 'cookie', 'set-cookie', 'x-api-key',
    'x-auth-token', 'proxy-authorization',
  ]);

  reqHeaders(): { name: string; value: string }[] {
    return this.flattenHeaders(this.detail.req_headers);
  }

  respHeaders(): { name: string; value: string }[] {
    return this.flattenHeaders(this.detail.resp_headers);
  }

  private flattenHeaders(h: Record<string, string[]> | undefined): { name: string; value: string }[] {
    if (!h) return [];
    return Object.entries(h).flatMap(([name, values]) =>
      values.map(value => ({ name: name.toLowerCase(), value }))
    );
  }

  isSensitive(name: string): boolean {
    return this.sensitiveNames.has(name.toLowerCase());
  }

  toggleMask(name: string, e: Event): void {
    e.stopPropagation();
    if (this.masked.has(name)) this.masked.delete(name);
    else this.masked.add(name);
  }

  copy(text: string): void {
    navigator.clipboard?.writeText(text).catch(() => {});
  }
}
