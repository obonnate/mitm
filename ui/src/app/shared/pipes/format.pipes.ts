import { Pipe, PipeTransform } from '@angular/core';

export type StatusClass = 'status-1xx' | 'status-2xx' | 'status-3xx' | 'status-4xx' | 'status-5xx' | 'status-err' | 'status-0';

@Pipe({ name: 'statusClass', standalone: true })
export class StatusClassPipe implements PipeTransform {
  transform(status: number | undefined): StatusClass {
    if (!status) return 'status-0';
    if (status < 200) return 'status-1xx';
    if (status < 300) return 'status-2xx';
    if (status < 400) return 'status-3xx';
    if (status < 500) return 'status-4xx';
    return 'status-5xx';
  }
}

@Pipe({ name: 'methodClass', standalone: true })
export class MethodClassPipe implements PipeTransform {
  transform(method: string | undefined): string {
    return `method-${(method ?? 'get').toLowerCase()}`;
  }
}

@Pipe({ name: 'bytes', standalone: true })
export class BytesPipe implements PipeTransform {
  transform(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1024 * 1024)).toFixed(2)} MB`;
  }
}

@Pipe({ name: 'duration', standalone: true })
export class DurationPipe implements PipeTransform {
  transform(ms: number): string {
    if (ms < 1000) return `${Math.round(ms)} ms`;
    return `${(ms / 1000).toFixed(2)} s`;
  }
}

@Pipe({ name: 'shortTime', standalone: true })
export class ShortTimePipe implements PipeTransform {
  transform(iso: string): string {
    if (!iso) return '';
    const d = new Date(iso);
    return d.toLocaleTimeString('en-GB', {
      hour: '2-digit', minute: '2-digit', second: '2-digit',
      fractionalSecondDigits: 3,
    });
  }
}
