import { apiCall } from './client';

export interface InboundCmd {
  accessory_id: number;
  quantity: number;
  unit_cost?: number;
  remark?: string;
  occurred_at?: string;
  client_ref?: string;
  // When true the backend treats `quantity` as the target absolute stock
  // level (set-to-X semantics) instead of an additive delta. Calibration
  // flows are recorded with a "[校准]" remark prefix so the flows page
  // can distinguish them from regular in/out rows.
  calibration?: boolean;
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
  return apiCall('POST', '/api/v1/stock/batch_inbound', items);
}

export function batchOutbound(items: OutboundCmd[]): Promise<BatchResult> {
  return apiCall('POST', '/api/v1/stock/batch_outbound', items);
}

// ── File outbound ────────────────────────────────────────────────

export interface FileOutboundPreviewItem {
  accessory_id: number;
  name: string;
  quantity: number;
  current_stock: number;
}

export interface FileOutboundNotFound {
  name: string;
  quantity: number;
}

export interface FileOutboundPreview {
  items: FileOutboundPreviewItem[];
  not_found: FileOutboundNotFound[];
  total_items: number;
  matched_count: number;
  not_found_count: number;
}

export async function previewFileOutbound(file: File): Promise<FileOutboundPreview> {
  const form = new FormData();
  form.append('file', file);
  const res = await fetch('/api/v1/stock/file_outbound', { method: 'POST', body: form });
  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try { errBody = await res.json(); } catch {}
    throw errBody;
  }
  return res.json();
}

export interface FileForceOutboundResult {
  outbound: number;
  created: number;
  shortages: number;
  flows: InventoryFlow[];
  ids: number[];
}

export async function executeFileOutbound(file: File): Promise<FileForceOutboundResult> {
  const form = new FormData();
  form.append('file', file);
  const res = await fetch('/api/v1/stock/file_outbound/execute', { method: 'POST', body: form });
  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try { errBody = await res.json(); } catch {}
    throw errBody;
  }
  return res.json();
}

export interface FileInboundItem {
  name: string;
  quantity: number;
  accessory_id: number;
  created: boolean;
  flow_id: number;
  balance_after: number;
}

export interface FileInboundResult {
  inbound: number;
  created: number;
  total_items: number;
  items: FileInboundItem[];
}

export async function executeFileInbound(file: File): Promise<FileInboundResult> {
  const form = new FormData();
  form.append('file', file);
  const res = await fetch('/api/v1/stock/file_inbound', { method: 'POST', body: form });
  if (!res.ok) {
    let errBody: any = { error: { code: 'INTERNAL', message: `HTTP ${res.status}` } };
    try { errBody = await res.json(); } catch {}
    throw errBody;
  }
  return res.json();
}