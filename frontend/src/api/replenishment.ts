import { apiCall } from './client';

export interface ReplenishmentItem {
  accessory_id: number;
  name: string;
  stall: string;
  current_stock: number;
  threshold: number;
  shortage: number;
  suggested_quantity: number;
}

export interface BatchCheckResult {
  items: ReplenishmentItem[];
  not_found: string[];
}

export function scan(): Promise<{ items: ReplenishmentItem[] }> {
  return apiCall('GET', '/api/v1/replenishment/scan');
}

export function check(names: string[], policy?: string): Promise<BatchCheckResult> {
  return apiCall('POST', '/api/v1/replenishment/check', { names, policy });
}

// exportReplenishment fetches the scan result as an .xlsx blob. Goes
// through fetch directly instead of apiCall because the response is
// binary, not JSON. Throws the same { error: { code, message } } envelope
// shape as apiCall on non-2xx so callers can surface the backend error
// uniformly.
export async function exportReplenishment(): Promise<Blob> {
  const res = await fetch('/api/v1/replenishment/export', { method: 'GET' });
  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try { errBody = await res.json(); } catch {}
    throw errBody;
  }
  return res.blob();
}