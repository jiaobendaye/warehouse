import { apiCall } from './client';

export interface InboundCmd {
  accessory_id: number;
  quantity: number;
  unit_cost?: number;
  remark?: string;
  occurred_at?: string;
  client_ref?: string;
}

export interface OutboundCmd {
  accessory_id: number;
  quantity: number;
  unit_price?: number;
  remark?: string;
  occurred_at?: string;
  client_ref?: string;
}

export interface InventoryFlow {
  id: number;
  accessory_id: number;
  type: 'in' | 'out';
  quantity: number;
  unit_cost: number;
  unit_price: number;
  balance_after: number;
  client_ref: string;
  remark: string;
  occurred_at: string;
  created_at: string;
}

export interface BatchResult {
  accepted: number;
  flows: InventoryFlow[];
}

export function inbound(cmd: InboundCmd): Promise<InventoryFlow> {
  return apiCall('POST', '/api/v1/stock/inbound', cmd);
}

export function outbound(cmd: OutboundCmd): Promise<InventoryFlow> {
  return apiCall('POST', '/api/v1/stock/outbound', cmd);
}

export function batchInbound(items: InboundCmd[]): Promise<BatchResult> {
  return apiCall('POST', '/api/v1/stock/batch_inbound', { items });
}

export function batchOutbound(items: OutboundCmd[]): Promise<BatchResult> {
  return apiCall('POST', '/api/v1/stock/batch_outbound', { items });
}