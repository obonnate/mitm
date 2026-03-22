import {
  Injectable, inject, signal, computed, DestroyRef,
} from '@angular/core';
import { takeUntilDestroyed, toObservable } from '@angular/core/rxjs-interop';
import { Subject } from 'rxjs';
import { debounceTime, distinctUntilChanged, switchMap } from 'rxjs/operators';
import { ProxyApiService } from './proxy-api.service';
import { WsService } from './ws.service';
import { ExchangeSummary, ListFilter } from './models';

@Injectable({ providedIn: 'root' })
export class ExchangeStore {
  private readonly api       = inject(ProxyApiService);
  private readonly ws        = inject(WsService);
  private readonly destroyRef = inject(DestroyRef);

  // ── State signals ──────────────────────────────────────────────────────────
  readonly exchanges    = signal<ExchangeSummary[]>([]);
  readonly total        = signal<number>(0);
  readonly selectedUuid = signal<string | null>(null);
  readonly loading      = signal<boolean>(false);
  readonly paused       = signal<boolean>(false);
  readonly filter       = signal<ListFilter>({ limit: 200, offset: 0 });

  // ── Derived ────────────────────────────────────────────────────────────────
  readonly selected = computed(() =>
    this.exchanges().find(e => e.uuid === this.selectedUuid()) ?? null,
  );

  readonly connected$ = this.ws.connected$;

  constructor() {
    // Re-fetch when filter changes.
    toObservable(this.filter).pipe(
      debounceTime(150),
      distinctUntilChanged((a, b) => JSON.stringify(a) === JSON.stringify(b)),
      switchMap(f => {
        this.loading.set(true);
        return this.api.list(f);
      }),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe(resp => {
      this.exchanges.set(resp.exchanges);
      this.total.set(resp.total);
      this.loading.set(false);
    });

    // Prepend live exchanges from the WebSocket stream.
    this.ws.exchange$.pipe(
      takeUntilDestroyed(this.destroyRef),
    ).subscribe(ex => {
      if (this.paused()) return;
      if (this.hasActiveFilter()) {
        this.triggerReload();
        return;
      }
      this.exchanges.update(list => {
        if (list.some(e => e.uuid === ex.uuid)) return list;
        // Prepend and cap at 2000 to avoid unbounded growth.
        return [ex, ...list].slice(0, 2000);
      });
      this.total.update(n => n + 1);
    });
  }

  // ── Public API ─────────────────────────────────────────────────────────────

  select(uuid: string | null): void {
    this.selectedUuid.set(uuid);
  }

  setFilter(patch: Partial<ListFilter>): void {
    this.filter.update(f => ({ ...f, ...patch, offset: 0 }));
  }

  clearFilter(): void {
    this.filter.set({ limit: 200, offset: 0 });
  }

  clear(): void {
    this.exchanges.set([]);
    this.total.set(0);
    this.selectedUuid.set(null);
  }

  togglePause(): void {
    this.paused.update(p => !p);
  }

  // ── Internal ───────────────────────────────────────────────────────────────

  /** Nudge the filter signal so subscribers re-fetch. */
  private triggerReload(): void {
    this.filter.update(f => ({ ...f }));
  }

  private hasActiveFilter(): boolean {
    const f = this.filter();
    return !!(f.host || f.method || f.search || f.status_min || f.status_max);
  }
}
