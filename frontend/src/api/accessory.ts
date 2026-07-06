import { apiCall } from './client';

export interface Accessory {
  id: number;
  name: string;
  current_stock: number;
  low_stock_threshold: number;
  notes: string;
  created_at: string;
  updated_at: string;
}

export interface AccessoryCreate {
  name: string;
  low_stock_threshold: number;
  notes?: string;
}

export interface AccessoryUpdate {
  name?: string;
  low_stock_threshold?: number;
  notes?: string;
}

export interface AccessoryListResponse {
  items: Accessory[];
  total: number;
  limit: number;
  offset: number;
}

export function listAccessories(q?: string, limit = 50, offset = 0): Promise<AccessoryListResponse> {
  const params = new URLSearchParams();
  if (q) params.set('q', q);
  params.set('limit', String(limit));
  params.set('offset', String(offset));
  return apiCall('GET', `/api/v1/accessories?${params}`);
}

export function getAccessory(id: number): Promise<Accessory> {
  return apiCall('GET', `/api/v1/accessories/${id}`);
}

export function createAccessory(data: AccessoryCreate): Promise<Accessory> {
  return apiCall('POST', '/api/v1/accessories', data);
}

export function updateAccessory(id: number, data: AccessoryUpdate): Promise<Accessory> {
  return apiCall('PATCH', `/api/v1/accessories/${id}`, data);
}

export function deleteAccessory(id: number): Promise<void> {
  return apiCall('DELETE', `/api/v1/accessories/${id}`);
}

// exportAccessories fetches the full inventory as an .xlsx blob. Goes
// through fetch directly instead of apiCall because the response is
// binary, not JSON. Throws the same { error: { code, message } } envelope
// shape as apiCall on non-2xx so callers can surface the backend error
// uniformly.
export async function exportAccessories(): Promise<Blob> {
  const res = await fetch('/api/v1/accessories/export', { method: 'GET' });
  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try { errBody = await res.json(); } catch {}
    throw errBody;
  }
  return res.blob();
}