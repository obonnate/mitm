export type Protocol = 'HTTP/1.1' | 'HTTP/2' | 'WebSocket' | 'TUNNEL';

export interface ExchangeSummary {
  id: number;
  uuid: string;
  protocol: Protocol;
  method: string;
  host: string;
  path: string;
  status: number;
  duration_ms: number;
  req_size: number;
  resp_size: number;
  tls: boolean;
  tags: string[];
  error?: string;
  started_at: string;
}

export interface TlsDetail {
  version: string;
  cipher_suite: string;
  server_name: string;
  alpn: string;
  forged_cert_der?: number[];
  upstream_cert_der?: number[];
}

export interface TimingDetail {
  started_at: string;
  duration_ms: number;
  dns_ms?: number;
  tcp_ms?: number;
  tls_ms?: number;
  ttfb_ms?: number;
  download_ms?: number;
}

export interface ExchangeDetail extends ExchangeSummary {
  req_headers: Record<string, string[]>;
  resp_headers: Record<string, string[]>;
  req_body: string;
  resp_body: string;
  tls_info?: TlsDetail;
  timing: TimingDetail;
}

export interface ListResponse {
  total: number;
  exchanges: ExchangeSummary[];
}

export interface WsMessage {
  type: 'exchange' | 'ping';
  data?: ExchangeSummary;
}

export interface ReplayRequest {
  method?: string;
  url?: string;
  headers?: Record<string, string>;
  body?: string;
}

export interface ReplayResponse {
  status: number;
  headers: Record<string, string[]>;
  body: string;
  error?: string;
}

export interface ListFilter {
  host?: string;
  method?: string;
  search?: string;
  status_min?: number;
  status_max?: number;
  limit?: number;
  offset?: number;
}
