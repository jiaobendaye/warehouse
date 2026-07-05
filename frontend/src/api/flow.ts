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

export function listFlows(params: FlowListParams = {}): Promise<FlowListResponse> {
  const sp = new URLSearchParams();
  if (params.accessory_id) sp.set('accessory_id', String(params.accessory_id));
  if (params.type) sp.set('type', params.type);
  if (params.from) sp.set('from', params.from);
  if (params.to) sp.set('to', params.to);
  if (params.limit) sp.set('limit', String(params.limit));
  if (params.offset) sp.set('offset', String(params.offset));
  return apiCall('GET', `/api/v1/flows?${sp}`);
}

export function getFlow(id: number): Promise<InventoryFlow> {
  return apiCall('GET', `/api/v1/flows/${id}`);
}