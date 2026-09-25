import { useEffect, useMemo, useState } from 'react';
import { request } from '../api/client';
import { getPrintRunRework, reworkPrintRun } from '../api/print-run';
import { roleAtLeast, useAuth } from '../hooks/useAuth';
import { usePagination } from '../hooks/usePagination';
import type { DomainRecord, EntityConfig, ReworkInfo } from '../types/domain';
import type { RunState } from '../types/status';
import type { EntityStore } from '../stores/factory';
import { formatDate } from '../utils/format';
import { StatusBadge } from './common/StatusBadge';
import { RunStateBadge } from './common/RunStateBadge';
import { ColorTable } from './common/ColorTable';
import { EmptyState } from './common/EmptyState';
import { MetricCard } from './common/MetricCard';
import { ConfirmDialog } from './common/ConfirmDialog';
import { UiButton } from './common/UiButton';

function decisionRunState(status: string): RunState {
  if (status === 'release') return 'released';
  if (status === 'rework' || status === 'quarantine') return 'hold';
  return 'proofing';
}

function nextPermittedStatus(config: EntityConfig, current: string, reviewer: boolean): string | null {
  const transitions: Record<string, Record<string, string | null>> = {
    pressUnit: { ready: 'setup', setup: 'printing', printing: 'maintenance', maintenance: 'printing' },
    printRun: { setup: 'printing', printing: 'proofing', proofing: reviewer ? 'released' : 'hold', hold: 'proofing', released: null },
    colorProof: { captured: 'review', review: reviewer ? 'accepted' : null, accepted: reviewer ? 'review' : null, rejected: reviewer ? 'review' : null },
    releaseDecision: { draft: reviewer ? 'release' : 'rework', release: reviewer ? 'rework' : null, rework: reviewer ? 'release' : null, quarantine: reviewer ? 'rework' : null },
  };
  return transitions[config.key]?.[current] ?? null;
}

export function EntityPage({ config, useStore }: { config: EntityConfig; useStore: EntityStore }) {
  const { session } = useAuth();
  const { items, meta, loading, error, load, createRecord, transition } = useStore();
  const [search, setSearch] = useState('');
  const [submittedSearch, setSubmittedSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; status: string } | null>(null);
  const [detail, setDetail] = useState<DomainRecord | null>(null);
  const [reworkTarget, setReworkTarget] = useState<DomainRecord | null>(null);
  const [reworkReason, setReworkReason] = useState('');
  const [reworkError, setReworkError] = useState('');
  const [reworkInfo, setReworkInfo] = useState<ReworkInfo | null>(null);
  const { page, pageSize, pages, setPage, previous, next } = usePagination(meta.total);
  const canWrite = roleAtLeast(session?.role, 'operator');
  const canReview = roleAtLeast(session?.role, 'reviewer');

  useEffect(() => { void load(config.path, submittedSearch, page, pageSize); }, [config.path, load, page, pageSize, submittedSearch]);
  const highRisk = useMemo(() => items.filter((item) => ['high', 'critical'].includes(item.riskLevel)).length, [items]);
  const createDemo = async () => {
    const now = Date.now();
    await createRecord(config.path, { code: `${config.key.toUpperCase()}-${now.toString().slice(-6)}`, name: `新增${config.label}`,
      description: '通过前端工作台创建的业务记录', facility: '默认作业区', owner: session?.username || 'operator', category: '常规', riskLevel: 'medium',
      metricValue: 2.4, metricUnit: 'ΔE', effectiveAt: new Date().toISOString(), evidence: '已完成创建前色彩检查', relatedCode: 'PR-001' });
    setShowCreate(false);
  };
  const openDetail = async (item: DomainRecord) => {
    setReworkInfo(null);
    try {
      const record = (await request<DomainRecord>(`/${config.path}/${item.id}`)).data;
      setDetail(record);
      if (config.key === 'printRun' && (record.reworkCount ?? 0) > 0) {
        try { setReworkInfo((await getPrintRunRework(record.id)).data); } catch { setReworkInfo(null); }
      }
    } catch { setDetail(item); }
  };
  const closeDetail = () => { setDetail(null); setReworkInfo(null); };
  const openRework = (item: DomainRecord) => { setReworkTarget(item); setReworkReason(''); setReworkError(''); };
  const submitRework = async () => {
    if (!reworkTarget) return;
    if (!reworkReason.trim()) { setReworkError('请填写返修原因后再提交'); return; }
    try {
      await reworkPrintRun(reworkTarget.id, reworkTarget.version, reworkReason.trim());
      setReworkTarget(null);
      await load(config.path, submittedSearch, page, pageSize);
    } catch (cause) { setReworkError(cause instanceof Error ? cause.message : String(cause)); }
  };

  return <main className="workspace">
    <header className="page-header"><div><p className="eyebrow">业务工作台</p><h1>{config.label}</h1><p>统一管理{config.label}的状态、风险、证据与责任人。</p></div>{canWrite && <UiButton onClick={() => setShowCreate(true)}>新增{config.label}</UiButton>}</header>
    <section className="metrics"><MetricCard label="记录总数" value={meta.total} detail="当前筛选范围"/><MetricCard label="高风险" value={highRisk} detail="需要优先复核"/><MetricCard label="状态种类" value={new Set(items.map((item) => item.status)).size} detail="状态机覆盖"/></section>
    {(config.key === 'colorProof' || config.key === 'releaseDecision') && <ColorTable records={items} title={config.key === 'colorProof' ? '当前校样读数' : '放行依据读数'} />}
    <section className="toolbar"><input aria-label="搜索" placeholder={`搜索${config.label}编码或名称`} value={search} onChange={(event) => setSearch(event.target.value)} /><UiButton onClick={() => { setPage(1); setSubmittedSearch(search); }}>查询</UiButton><button className="link-button" onClick={() => { setSearch(''); setSubmittedSearch(''); setPage(1); }}>重置</button></section>
    {error && <div className="alert" role="alert">{error}</div>}
    <section className="table-shell" aria-busy={loading}><table><thead><tr><th>编码</th><th>名称</th><th>状态</th><th>风险</th><th>责任人</th><th>指标</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => { const target = nextPermittedStatus(config, item.status, canReview); return <tr key={item.id}><td><strong>{item.code}</strong></td><td><button className="record-link" onClick={() => void openDetail(item)}>{item.name}</button><small>{item.facility}</small></td><td>{config.key === 'printRun' ? <RunStateBadge state={item.status as RunState}/> : <StatusBadge status={item.status}/>} {config.key === 'releaseDecision' && <RunStateBadge state={decisionRunState(item.status)}/>}{config.key === 'printRun' && (item.reworkCount ?? 0) > 0 && <small className="rework-tag">返修×{item.reworkCount}</small>}</td><td>{item.riskLevel}</td><td>{item.owner}</td><td>{item.metricValue} {item.metricUnit}</td><td>{formatDate(item.updatedAt)}</td><td>{canWrite && target ? <button className="table-action" onClick={() => setPending({ item, status: target })}>推进至 {target}</button> : <button className="table-action" onClick={() => void openDetail(item)}>查看详情</button>}{config.key === 'printRun' && item.status === 'released' && canReview && <button className="table-action" onClick={() => openRework(item)}>发起返修</button>}</td></tr>; })}
      {!items.length && !loading && <tr><td colSpan={8}><EmptyState title="没有匹配记录" detail="可清空搜索条件后重新查询" /></td></tr>}
    </tbody></table>{loading && <div className="loading">正在同步业务数据…</div>}</section>
    <footer className="pagination"><button onClick={previous} disabled={page <= 1}>上一页</button><span>第 {page} / {pages} 页</span><button onClick={next} disabled={page >= pages}>下一页</button></footer>
    <ConfirmDialog open={showCreate} title={`新增${config.label}`} onCancel={() => setShowCreate(false)} onConfirm={() => void createDemo()}><p>将创建一条包含完整责任人、风险和证据信息的演示记录。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending)} title="确认状态迁移" onCancel={() => setPending(null)} onConfirm={() => { if (pending) void transition(config.path, pending.item, pending.status).then(() => setPending(null)); }}><p>状态迁移会写入审计日志；色彩配置和放行决定同时生成不可变版本。</p><strong>{pending?.item.status} → {pending?.status}</strong></ConfirmDialog>
    <ConfirmDialog open={Boolean(reworkTarget)} title={`发起批次返修 · ${reworkTarget?.code || ''}`} onCancel={() => setReworkTarget(null)} onConfirm={() => void submitRework()}>
      <p>复核员填写原因并确认后，批次进入等待状态，记录返修开始时间，该批次此前校样全部失效；为同一批次补做校样并通过复核后才可再次放行。</p>
      <textarea className="rework-reason" aria-label="返修原因" placeholder="请填写返修原因（必填）" rows={3} value={reworkReason} onChange={(event) => setReworkReason(event.target.value)} />
      {reworkError && <div className="alert" role="alert">{reworkError}</div>}
    </ConfirmDialog>
    <ConfirmDialog open={Boolean(detail)} title={`${detail?.code || ''} 记录详情`} onCancel={closeDetail} onConfirm={closeDetail}>{detail && <div className="detail-content"><p>{detail.description}</p><dl><div><dt>证据</dt><dd>{detail.evidence || '-'}</dd></div><div><dt>当前版本</dt><dd>v{detail.version}</dd></div></dl><ColorTable records={[detail]} title="记录色彩读数" />{reworkInfo && <section className="rework-panel"><h3>批次返修</h3><dl><div><dt>开始时间</dt><dd>{reworkInfo.reworkStartedAt ? formatDate(reworkInfo.reworkStartedAt) : '-'}</dd></div><div><dt>累计返修</dt><dd>{reworkInfo.reworkCount} 次</dd></div></dl><ColorTable records={reworkInfo.invalidatedProofs.map((proof) => ({ ...proof, relatedCode: reworkInfo.runCode }) as DomainRecord)} title={`失效校样（${reworkInfo.invalidatedProofs.length}）`} />{!reworkInfo.invalidatedProofs.length && <p className="muted">本次返修前没有关联校样。</p>}<h3>再次放行条件</h3><ul className="condition-list">{reworkInfo.reReleaseConditions.map((condition) => <li key={condition.label} className={condition.met ? 'met' : 'unmet'}><i>{condition.met ? '✓' : '✗'}</i>{condition.label}</li>)}</ul><p className={reworkInfo.reReleaseReady ? 'rework-ready' : 'muted'}>{reworkInfo.reReleaseReady ? '已满足再次放行条件，可由复核员放行。' : '尚未满足再次放行条件。'}</p></section>}{detail.revisions?.length ? <div className="revision-list"><h3>版本链</h3>{detail.revisions.map((revision) => <article key={revision.id}><strong>v{revision.version} · {revision.status}</strong><span>{revision.actor} · {revision.reason}</span><code>{revision.requestId}</code></article>)}</div> : null}</div>}</ConfirmDialog>
  </main>;
}
