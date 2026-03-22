import { Component, Input, ChangeDetectionStrategy, computed, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { TimingDetail } from '../../../core/models';

interface Phase {
  label: string;
  ms: number;
  color: string;
  offsetPct: number;
  widthPct: number;
}

@Component({
  selector: 'gp-timeline-pane',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule],
  template: `
    <div class="tl-wrap">
      <div class="tl-total">Total: <strong>{{ timing.duration_ms.toFixed(1) }} ms</strong></div>

      <div class="tl-chart">
        <div class="tl-row" *ngFor="let p of phases()">
          <span class="tl-label">{{ p.label }}</span>
          <div class="tl-track">
            <div
              class="tl-fill"
              [style.left.%]="p.offsetPct"
              [style.width.%]="p.widthPct || 0.5"
              [style.background]="p.color"
            ></div>
          </div>
          <span class="tl-ms">{{ p.ms.toFixed(1) }} ms</span>
        </div>
      </div>

      <div class="tl-legend">
        <div class="legend-item" *ngFor="let p of phases()">
          <span class="legend-dot" [style.background]="p.color"></span>
          <span>{{ p.label }}</span>
        </div>
      </div>

      <div class="tl-stats">
        <div class="stat" *ngFor="let s of stats()">
          <span class="stat-label">{{ s.label }}</span>
          <span class="stat-value">{{ s.value }}</span>
        </div>
      </div>
    </div>
  `,
  styles: [`
    .tl-wrap { padding: 16px; }
    .tl-total { font-size: 13px; color: var(--text-muted); margin-bottom: 20px; }
    .tl-total strong { color: var(--text); }

    .tl-chart { display: flex; flex-direction: column; gap: 8px; }
    .tl-row { display: flex; align-items: center; gap: 10px; }
    .tl-label {
      font-size: 11px; color: var(--text-muted); width: 80px;
      text-align: right; flex-shrink: 0; font-family: var(--font-mono);
    }
    .tl-track {
      flex: 1; height: 10px; background: var(--bg-3);
      border-radius: 5px; position: relative; overflow: hidden;
    }
    .tl-fill {
      position: absolute; top: 0; height: 100%;
      border-radius: 5px; transition: all 0.3s ease;
      min-width: 3px;
    }
    .tl-ms {
      font-size: 11px; color: var(--text-muted); width: 65px;
      font-family: var(--font-mono); flex-shrink: 0;
    }

    .tl-legend {
      display: flex; flex-wrap: wrap; gap: 12px; margin-top: 16px; padding-top: 12px;
      border-top: 1px solid var(--border);
    }
    .legend-item { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text-muted); }
    .legend-dot { width: 8px; height: 8px; border-radius: 2px; flex-shrink: 0; }

    .tl-stats {
      display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
      gap: 1px; background: var(--border); border: 1px solid var(--border);
      border-radius: 6px; overflow: hidden; margin-top: 20px;
    }
    .stat {
      display: flex; flex-direction: column; gap: 2px; padding: 10px 12px;
      background: var(--bg-2);
    }
    .stat-label { font-size: 10px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-muted); }
    .stat-value { font-size: 14px; font-family: var(--font-mono); color: var(--text); }
  `],
})
export class TimelinePaneComponent {
  @Input() timing!: TimingDetail;

  phases = computed((): Phase[] => {
    const t = this.timing;
    const total = t.duration_ms || 1;
    const raw: { label: string; ms: number; color: string }[] = [];

    if (t.dns_ms)      raw.push({ label: 'DNS',      ms: t.dns_ms,      color: '#60a5fa' });
    if (t.tcp_ms)      raw.push({ label: 'TCP',       ms: t.tcp_ms,      color: '#818cf8' });
    if (t.tls_ms)      raw.push({ label: 'TLS',       ms: t.tls_ms,      color: '#a78bfa' });
    if (t.ttfb_ms)     raw.push({ label: 'Server',    ms: t.ttfb_ms,     color: '#fbbf24' });
    if (t.download_ms) raw.push({ label: 'Download',  ms: t.download_ms, color: '#34d399' });

    if (!raw.length) {
      raw.push({ label: 'Total', ms: total, color: '#60a5fa' });
    }

    let cursor = 0;
    return raw.map(r => {
      const offsetPct = (cursor / total) * 100;
      const widthPct  = (r.ms / total) * 100;
      cursor += r.ms;
      return { ...r, offsetPct, widthPct };
    });
  });

  stats = computed(() => {
    const t = this.timing;
    return [
      { label: 'Total',    value: `${t.duration_ms.toFixed(1)} ms` },
      { label: 'TTFB',     value: t.ttfb_ms     ? `${t.ttfb_ms.toFixed(1)} ms`     : '—' },
      { label: 'Download', value: t.download_ms  ? `${t.download_ms.toFixed(1)} ms` : '—' },
      { label: 'TLS',      value: t.tls_ms       ? `${t.tls_ms.toFixed(1)} ms`      : '—' },
    ];
  });
}
