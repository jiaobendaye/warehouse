import { apiCall } from './client';
import type { InventoryFlow } from './stock';

export interface FlowListParams {
  accessory_id?: number;
  type?: 'in' | 'out';
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
}

export interface FlowListResponse {
  items: InventoryFlow[];
  total: number;
  limit: number;
  offset: number;
}

// toDateRange converts a date-only value ("YYYY-MM-DD", as produced by
// <input type="date">) into an RFC3339 timestamp the backend accepts.
// `end` controls whether the time is clamped to start (00:00:00Z) or
// end (23:59:59Z) of that day. Values already in RFC3339 form pass through.
function toDateRange(v: string, end: boolean): string {
  if (!v) return v;
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) {
    return v + (end ? 'T23:59:59Z' : 'T00:00:00Z');
  }
  return v;
}

export function listFlows(params: FlowListParams = {}): Promise<FlowListResponse> {
  const sp = new URLSearchParams();
  if (params.accessory_id) sp.set('accessory_id', String(params.accessory_id));
  if (params.type) sp.set('type', params.type);
  const from = toDateRange(params.from || '', false);
  const to = toDateRange(params.to || '', true);
  if (from) sp.set('from', from);
  if (to) sp.set('to', to);
  if (params.limit) sp.set('limit', String(params.limit));
  if (params.offset) sp.set('offset', String(params.offset));
  return apiCall('GET', `/api/v1/flows?${sp}`);
}

export function getFlow(id: number): Promise<InventoryFlow> {
  return apiCall('GET', `/api/v1/flows/${id}`);
}