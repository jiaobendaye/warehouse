// Transport adapter: detects whether we're inside Wails WebView or a browser.
// In Wails mode, calls go through Wails bindings (Batch 12).
// In browser mode, calls go through fetch(/api/v1/...).

export function isWails(): boolean {
  return !!(window as any).runtime || !!(window as any).go;
}

export type Transport = 'wails' | 'http';

export function getTransport(): Transport {
  return isWails() ? 'wails' : 'http';
}

export async function apiCall<T = any>(
  method: string,
  path: string,
  body?: any
): Promise<T> {
  if (isWails()) {
    throw new Error('Wails bindings not available yet — use browser mode');
  }

  const url = path.startsWith('http') ? path : path.startsWith('/') ? path : `/api/v1/${path}`;
  const opts: RequestInit = {
    method: method.toUpperCase(),
    headers: { 'Content-Type': 'application/json' },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }

  const res = await fetch(url, opts);
  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try {
      errBody = await res.json();
    } catch {
      // keep default error
    }
    throw errBody;
  }
  return res.json();
}