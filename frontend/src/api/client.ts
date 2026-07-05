// Transport adapter: detects whether we're inside Wails WebView or a browser.
//
// In Wails (desktop) mode, the asset server proxies /api/* and /mcp/* to
// the embedded HTTP server (see internal/desktop/proxy.go), so the same
// fetch path works for both GUI and browser. The only thing the Wails
// environment adds is the existence of window.runtime / window.go — used
// by Settings.tsx to decide whether to show the server toggle button.

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
  const url = path.startsWith('http') ? path : path.startsWith('/') ? path : `/api/v1/${path}`;
  const opts: RequestInit = {
    method: method.toUpperCase(),
    headers: { 'Content-Type': 'application/json' },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }

  let res: Response;
  try {
    res = await fetch(url, opts);
  } catch (e: any) {
    // fetch itself rejects on network errors (DNS, connection refused,
    // mixed-content in webview, etc.). Surface the real cause so it's
    // visible in DevTools instead of being swallowed by the fallback toast.
    console.error(`[apiCall] network error ${method} ${url}:`, e);
    throw {
      error: {
        code: 'NETWORK',
        message: e?.message || `network error: ${method} ${url}`,
      },
    };
  }

  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try {
      errBody = await res.json();
    } catch {
      // keep default error
    }
    console.error(`[apiCall] HTTP ${res.status} ${method} ${url}:`, errBody);
    throw errBody;
  }
  return res.json();
}