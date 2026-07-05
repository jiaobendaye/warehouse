import { apiCall } from './client';

export interface ReplenishmentItem {
  accessory_id: number;
  name: string;
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