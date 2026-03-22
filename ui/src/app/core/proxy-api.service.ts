import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import {
  ExchangeDetail,
  ListFilter,
  ListResponse,
  ReplayRequest,
  ReplayResponse,
} from './models';

@Injectable({ providedIn: 'root' })
export class ProxyApiService {
  private readonly http = inject(HttpClient);
  private readonly base = '/api';

  list(filter: ListFilter = {}): Observable<ListResponse> {
    let params = new HttpParams();
    if (filter.host)       params = params.set('host',       filter.host);
    if (filter.method)     params = params.set('method',     filter.method);
    if (filter.search)     params = params.set('search',     filter.search);
    if (filter.status_min) params = params.set('status_min', filter.status_min);
    if (filter.status_max) params = params.set('status_max', filter.status_max);
    if (filter.limit)      params = params.set('limit',      filter.limit);
    if (filter.offset)     params = params.set('offset',     filter.offset ?? 0);
    return this.http.get<ListResponse>(`${this.base}/exchanges`, { params });
  }

  get(uuid: string): Observable<ExchangeDetail> {
    return this.http.get<ExchangeDetail>(`${this.base}/exchanges/${uuid}`);
  }

  replay(uuid: string, req: ReplayRequest): Observable<ReplayResponse> {
    return this.http.post<ReplayResponse>(
      `${this.base}/exchanges/${uuid}/replay`,
      req,
    );
  }

  caCertUrl(): string {
    return `${this.base}/ca.crt`;
  }

  stats(): Observable<{ store: string; ws_clients: number }> {
    return this.http.get<{ store: string; ws_clients: number }>(
      `${this.base}/stats`,
    );
  }
}
