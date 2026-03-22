import { Injectable, OnDestroy, NgZone, inject } from '@angular/core';
import { Subject, Observable, timer, EMPTY } from 'rxjs';
import { webSocket, WebSocketSubject } from 'rxjs/webSocket';
import { catchError, switchMap, filter, map, shareReplay } from 'rxjs/operators';
import { ExchangeSummary, WsMessage } from './models';

const RECONNECT_MS = 3_000;

@Injectable({ providedIn: 'root' })
export class WsService implements OnDestroy {
  private readonly zone = inject(NgZone);
  private socket$?: WebSocketSubject<WsMessage>;

  /** true = connected, false = disconnected/reconnecting */
  readonly connected$ = new Subject<boolean>();

  /** Stream of new exchanges pushed by the proxy in real time. */
  readonly exchange$: Observable<ExchangeSummary>;

  constructor() {
    this.exchange$ = this.buildStream().pipe(
      filter((msg): msg is WsMessage & { type: 'exchange'; data: ExchangeSummary } =>
        msg.type === 'exchange' && msg.data != null,
      ),
      map(msg => msg.data!),
      shareReplay({ bufferSize: 0, refCount: true }),
    );
  }

  private buildStream(): Observable<WsMessage> {
    return new Observable<WsMessage>(observer => {
      const wsUrl = `ws://${location.host}/ws`;

      this.socket$ = webSocket<WsMessage>({
        url: wsUrl,
        openObserver:  { next: () => this.zone.run(() => this.connected$.next(true)) },
        closeObserver: { next: () => this.zone.run(() => this.connected$.next(false)) },
      });

      const sub = this.socket$.pipe(
        catchError(() => EMPTY),
      ).subscribe(observer);

      return () => sub.unsubscribe();
    }).pipe(
      catchError(() =>
        timer(RECONNECT_MS).pipe(switchMap(() => this.buildStream())),
      ),
    );
  }

  ngOnDestroy(): void {
    this.socket$?.complete();
  }
}
