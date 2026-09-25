
import { request } from './client';
import type { DomainRecord, ReworkDetail, ReworkRecord } from '../types/domain';

// 复核员对已放行批次发起返修：原因必填（缺失/状态不符/版本变化由后端返回 409）。
export async function startRunRework(runId: number, expectedVersion: number, reason: string) {
  return request<ReworkRecord>(`/runs/${runId}/rework`, {
    method: 'POST',
    body: JSON.stringify({ expectedVersion, reason }),
  });
}

export async function listReworks(page = 1, pageSize = 20, runCode = '') {
  const suffix = runCode ? `&runCode=${encodeURIComponent(runCode)}` : '';
  return request<ReworkRecord[]>(`/reworks?page=${page}&pageSize=${pageSize}${suffix}`);
}

export async function getRework(id: number) {
  return request<ReworkDetail>(`/reworks/${id}`);
}

// 返修等待中的补做校样：绑定到同一批次，随后按常规 captured -> review -> accepted 流转。
export async function createReplacementProof(input: Partial<DomainRecord>) {
  return request<DomainRecord>('/proofs', { method: 'POST', body: JSON.stringify(input) });
}
