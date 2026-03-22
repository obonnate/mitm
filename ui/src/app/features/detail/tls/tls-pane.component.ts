import { Component, Input, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { TlsDetail, ExchangeDetail } from '../../../core/models';

@Component({
  selector: 'gp-tls-pane',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule],
  template: `
    <div class="tls-wrap">
      <!-- Session grid -->
      <div class="field-grid">
        <div class="field" *ngFor="let f of sessionFields()">
          <span class="field-label">{{ f.label }}</span>
          <span class="field-value">{{ f.value }}</span>
        </div>
      </div>

      <!-- Forged cert -->
      <div class="cert-card">
        <div class="cert-head">
          <span class="cert-icon ok">✓</span>
          <span>Forged certificate <span class="cert-sub">(presented to client)</span></span>
        </div>
        <table class="cert-table">
          <tr *ngFor="let f of forgedFields()">
            <td class="cert-label">{{ f.label }}</td>
            <td class="cert-value">{{ f.value }}</td>
          </tr>
        </table>
      </div>

      <!-- Upstream cert -->
      <div class="cert-card">
        <div class="cert-head">
          <span class="cert-icon info">↑</span>
          <span>Upstream certificate <span class="cert-sub">(from origin server)</span></span>
        </div>
        <table class="cert-table">
          <tr *ngFor="let f of upstreamFields()">
            <td class="cert-label">{{ f.label }}</td>
            <td class="cert-value">{{ f.value }}</td>
          </tr>
        </table>
      </div>
    </div>
  `,
  styles: [`
    .tls-wrap { padding: 12px; display: flex; flex-direction: column; gap: 16px; }

    .field-grid {
      display: grid; grid-template-columns: 1fr 1fr; gap: 1px;
      background: var(--border); border: 1px solid var(--border); border-radius: 6px; overflow: hidden;
    }
    .field {
      display: flex; flex-direction: column; gap: 3px; padding: 10px 12px;
      background: var(--bg-2);
    }
    .field-label { font-size: 10px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-muted); }
    .field-value { font-family: var(--font-mono); font-size: 12px; color: var(--text); word-break: break-all; }

    .cert-card { border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
    .cert-head {
      display: flex; align-items: center; gap: 8px; padding: 8px 12px;
      background: var(--bg-2); border-bottom: 1px solid var(--border);
      font-size: 12px; color: var(--text);
    }
    .cert-icon {
      width: 18px; height: 18px; border-radius: 50%; display: flex;
      align-items: center; justify-content: center; font-size: 10px; font-weight: 700; flex-shrink: 0;
    }
    .cert-icon.ok   { background: rgba(34,197,94,0.15); color: #22c55e; }
    .cert-icon.info { background: rgba(96,165,250,0.15); color: #60a5fa; }
    .cert-sub { color: var(--text-muted); font-size: 11px; }

    .cert-table { width: 100%; border-collapse: collapse; }
    .cert-table tr:hover td { background: var(--bg-hover); }
    .cert-label {
      padding: 4px 12px; font-family: var(--font-mono); font-size: 11px;
      color: var(--text-muted); width: 38%; vertical-align: top; white-space: nowrap;
    }
    .cert-value {
      padding: 4px 12px 4px 0; font-family: var(--font-mono); font-size: 11px;
      color: var(--text); word-break: break-all;
    }
  `],
})
export class TlsPaneComponent {
  @Input() tlsInfo!: TlsDetail;
  @Input() detail!: ExchangeDetail;

  sessionFields(): { label: string; value: string }[] {
    return [
      { label: 'Protocol',    value: this.tlsInfo.version },
      { label: 'Cipher suite',value: this.tlsInfo.cipher_suite },
      { label: 'SNI',         value: this.tlsInfo.server_name },
      { label: 'ALPN',        value: this.tlsInfo.alpn || 'http/1.1' },
    ];
  }

  forgedFields(): { label: string; value: string }[] {
    return [
      { label: 'Common name', value: this.tlsInfo.server_name },
      { label: 'Issuer',      value: 'Mitm Local CA' },
      { label: 'Valid for',   value: '24 hours (per-session)' },
      { label: 'Key',         value: 'ECDSA P-256' },
    ];
  }

  upstreamFields(): { label: string; value: string }[] {
    if (!this.tlsInfo.upstream_cert_der?.length) {
      return [{ label: 'Status', value: 'Not captured' }];
    }
    return [
      { label: 'Host',   value: this.tlsInfo.server_name },
      { label: 'Status', value: 'Captured (DER available)' },
    ];
  }
}
