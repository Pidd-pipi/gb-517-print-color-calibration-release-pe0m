
import { request } from './client';
import type { DomainRecord, ReworkInfo } from '../types/domain';

export async function listPrintRun(page = 1, pageSize = 20, search = '') {
  return request<DomainRecord[]>(`/runs?page=${page}&pageSize=${pageSize}&search=${encodeURIComponent(search)}`);
}
export async function createPrintRun(input: Partial<DomainRecord>) {
  return request<DomainRecord>('/runs', { method: 'POST', body: JSON.stringify(input) });
}
export async function transitionPrintRun(id: number, status: string, expectedVersion: number, reason: string) {
  return request<DomainRecord>(`/runs/${id}/transition`, {
    method: 'POST', body: JSON.stringify({ status, expectedVersion, reason }),
  });
}
export async function reworkPrintRun(id: number, expectedVersion: number, reason: string) {
  return request<DomainRecord>(`/runs/${id}/rework`, {
    method: 'POST', body: JSON.stringify({ expectedVersion, reason }),
  });
}
export async function getPrintRunRework(id: number) {
  return request<ReworkInfo>(`/runs/${id}/rework`);
}
