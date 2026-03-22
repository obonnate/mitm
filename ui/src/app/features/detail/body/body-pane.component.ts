import {
  Component, Input, ChangeDetectionStrategy, signal, computed, OnChanges,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { BytesPipe } from '../../../shared/pipes/format.pipes';

type ViewMode = 'pretty' | 'raw' | 'hex';

@Component({
  selector: 'gp-body-pane',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule, BytesPipe],
  template: `
    <div class="body-toolbar">
      <span class="ct-label">{{ contentType() }}</span>
      <span class="size-label">{{ body.length | bytes }}</span>
      <div class="flex-gap"></div>
      <div class="view-toggle">
        <button *ngFor="let m of modes" class="vbtn" [class.active]="mode() === m" (click)="mode.set(m)">{{ m }}</button>
      </div>
      <button class="copy-btn" (click)="copy()" [class.copied]="copied()">
        {{ copied() ? 'Copied!' : 'Copy' }}
      </button>
    </div>

    <div class="body-empty" *ngIf="!body">
      <span>No body</span>
    </div>

    <div class="code-wrap" *ngIf="body && mode() === 'pretty'">
      <pre class="code-pre" [innerHTML]="highlighted()"></pre>
    </div>

    <div class="code-wrap" *ngIf="body && mode() === 'raw'">
      <pre class="code-pre raw">{{ body }}</pre>
    </div>

    <div class="code-wrap hex-wrap" *ngIf="body && mode() === 'hex'">
      <div class="hex-row" *ngFor="let row of hexRows()">
        <span class="hex-offset">{{ row.offset }}</span>
        <span class="hex-bytes">{{ row.bytes }}</span>
        <span class="hex-ascii">{{ row.ascii }}</span>
      </div>
    </div>
  `,
  styles: [`
    :host { display: flex; flex-direction: column; height: 100%; }
    .body-toolbar {
      display: flex; align-items: center; gap: 8px; padding: 5px 10px;
      border-bottom: 1px solid var(--border); background: var(--bg-2); flex-shrink: 0;
    }
    .ct-label { font-size: 11px; color: var(--text-muted); font-family: var(--font-mono); }
    .size-label { font-size: 11px; color: var(--text-dim); font-family: var(--font-mono); }
    .flex-gap { flex: 1; }
    .view-toggle { display: flex; border: 1px solid var(--border); border-radius: 5px; overflow: hidden; }
    .vbtn {
      padding: 3px 9px; font-size: 11px; background: none; border: none;
      color: var(--text-muted); cursor: pointer; transition: all 0.1s;
    }
    .vbtn + .vbtn { border-left: 1px solid var(--border); }
    .vbtn.active { background: var(--bg-3); color: var(--text); }
    .copy-btn {
      font-size: 11px; padding: 3px 8px; border: 1px solid var(--border);
      border-radius: 4px; background: none; color: var(--text-muted); cursor: pointer;
      transition: all 0.12s;
    }
    .copy-btn:hover { color: var(--text); border-color: var(--text-muted); }
    .copy-btn.copied { color: var(--green); border-color: var(--green); }

    .body-empty {
      flex: 1; display: flex; align-items: center; justify-content: center;
      color: var(--text-muted); font-size: 12px;
    }
    .code-wrap { flex: 1; overflow: auto; }
    .code-pre {
      margin: 0; padding: 12px; font-family: var(--font-mono); font-size: 12px;
      line-height: 1.65; white-space: pre-wrap; word-break: break-all;
      color: var(--text);
    }
    .code-pre.raw { color: var(--text-muted); }

    /* JSON syntax colours */
    :host ::ng-deep .jk { color: #7dd3fc; }   /* key */
    :host ::ng-deep .js { color: #86efac; }   /* string value */
    :host ::ng-deep .jn { color: #fdba74; }   /* number */
    :host ::ng-deep .jb { color: #a78bfa; }   /* boolean/null */
    :host ::ng-deep .jp { color: var(--text-muted); } /* punctuation */

    .hex-wrap { padding: 8px 12px; }
    .hex-row { display: flex; gap: 16px; font-family: var(--font-mono); font-size: 11px; line-height: 1.7; }
    .hex-offset { color: var(--text-muted); width: 52px; flex-shrink: 0; }
    .hex-bytes  { color: var(--text-dim); flex: 1; letter-spacing: 0.04em; }
    .hex-ascii  { color: var(--text-muted); width: 130px; flex-shrink: 0; }
  `],
})
export class BodyPaneComponent implements OnChanges {
  @Input() body: string = '';
  @Input() headers: Record<string, string[]> = {};

  mode = signal<ViewMode>('pretty');
  copied = signal(false);
  readonly modes: ViewMode[] = ['pretty', 'raw', 'hex'];

  highlighted = computed(() => {
    if (!this.body) return '';
    if (this.isJson()) return this.highlightJson(this.body);
    return this.escapeHtml(this.body);
  });

  ngOnChanges(): void {
    // Reset to pretty when body changes.
    this.mode.set('pretty');
  }

  contentType(): string {
    const ct = Object.entries(this.headers)
      .find(([k]) => k.toLowerCase() === 'content-type')?.[1]?.[0] ?? '';
    return ct.split(';')[0].trim() || 'text/plain';
  }

  isJson(): boolean {
    const ct = this.contentType();
    return ct.includes('json') || (this.body.trimStart().startsWith('{') || this.body.trimStart().startsWith('['));
  }

  hexRows(): { offset: string; bytes: string; ascii: string }[] {
    const bytes = new TextEncoder().encode(this.body);
    const rows: { offset: string; bytes: string; ascii: string }[] = [];
    const COLS = 16;
    for (let i = 0; i < bytes.length; i += COLS) {
      const chunk = bytes.slice(i, i + COLS);
      const bytesStr = Array.from(chunk).map(b => b.toString(16).padStart(2, '0')).join(' ').padEnd(COLS * 3 - 1, ' ');
      const ascii = Array.from(chunk).map(b => b >= 32 && b < 127 ? String.fromCharCode(b) : '.').join('');
      rows.push({ offset: i.toString(16).padStart(8, '0'), bytes: bytesStr, ascii });
    }
    return rows;
  }

  copy(): void {
    navigator.clipboard?.writeText(this.body).then(() => {
      this.copied.set(true);
      setTimeout(() => this.copied.set(false), 1500);
    }).catch(() => {});
  }

  private highlightJson(src: string): string {
    try {
      const obj = JSON.parse(src);
      const pretty = JSON.stringify(obj, null, 2);
      return this.colorizeJson(pretty);
    } catch {
      return this.escapeHtml(src);
    }
  }

  private colorizeJson(json: string): string {
    return this.escapeHtml(json).replace(
      /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g,
      match => {
        if (/^"/.test(match)) {
          if (/:$/.test(match)) return `<span class="jk">${match}</span>`;
          return `<span class="js">${match}</span>`;
        }
        if (/true|false|null/.test(match)) return `<span class="jb">${match}</span>`;
        return `<span class="jn">${match}</span>`;
      },
    );
  }

  private escapeHtml(s: string): string {
    return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }
}
