import { useEffect, useState } from 'react';
import { request } from '../../api/client';
import { createReplacementProof, getRework, startRunRework } from '../../api/run-rework';
import { transitionPrintRun } from '../../api/print-run';
import type { DomainRecord, ReworkDetail, ReworkRecord } from '../../types/domain';
import { formatDate } from '../../utils/format';
import { ConfirmDialog } from './ConfirmDialog';
import { UiButton } from './UiButton';

// ReworkPanel renders the 批次返修 lifecycle inside a 印刷批次 detail: start
// rework for a released batch, show waiting state / start time / cumulative
// count / invalidated proofs, capture a replacement proof and perform the
// gated re-release once the new proof passed review.
export function ReworkPanel({ run: initialRun, reviewer }: { run: DomainRecord; reviewer: boolean }) {
  const [run, setRun] = useState<DomainRecord>(initialRun);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [showStart, setShowStart] = useState(false);
  const [showProof, setShowProof] = useState(false);
  const [reason, setReason] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => { setRun(initialRun); }, [initialRun]);

  const reload = async () => {
    const res = await request<DomainRecord>(`/runs/${initialRun.id}`);
    setRun(res.data);
    setReloadKey((k) => k + 1);
  };

  const guard = (action: () => Promise<void>) => async () => {
    setBusy(true); setError('');
    try {
      await action();
      setShowStart(false); setShowProof(false); setReason('');
      await reload();
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  };

  const start = guard(async () => { await startRunRework(run.id, run.version, reason); });
  const captureProof = guard(async () => {
    await createReplacementProof({
      code: `CP-RW-${Date.now().toString().slice(-8)}`,
      name: `${run.code} 返修补做校样`,
      description: '批次返修等待期间补做的同批次校样',
      facility: run.facility || '默认作业区', owner: 'operator', category: run.category || '校准',
      riskLevel: run.riskLevel || 'medium', metricValue: 1.4, metricUnit: 'ΔE',
      effectiveAt: new Date().toISOString(), evidence: '返修后重新分光采样',
      relatedCode: 'REWORK', runCode: run.code,
    });
  });

  return <section className="rework-panel">
    <h3>批次返修</h3>
    {error && <div className="alert" role="alert">{error}</div>}
    {!run.reworks?.length && <p className="rework-empty">该批次尚无返修记录。放行后发现色差可由复核员补批次返修：批次进入等待、此前校样失效，原放行决定与版本链保留。</p>}
    {run.reworks?.map((rework) => (
      <ReworkCard key={rework.id} rework={rework} run={run} reviewer={reviewer}
        reloadKey={reloadKey} reload={reload} busy={busy} setBusy={setBusy} setError={setError} />
    ))}

    <div className="rework-actions">
      {run.status === 'released' && reviewer && (
        <UiButton onClick={() => setShowStart(true)} disabled={busy}>发起批次返修</UiButton>
      )}
      {run.status === 'rework_pending' && (
        <UiButton onClick={() => setShowProof(true)} disabled={busy}>补做同批次校样</UiButton>
      )}
    </div>

    <ConfirmDialog open={showStart} title="发起批次返修" onCancel={() => setShowStart(false)} onConfirm={() => void start()}>
      <p>批次将进入<b>返修等待</b>状态，记录开始时间与累计返修次数，该批次此前校样全部失效；原放行决定与版本链保留。</p>
      <label className="rework-reason">色差/返修原因
        <textarea value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} placeholder="例如：放行后抽检发现 ΔE 超出容差" />
      </label>
    </ConfirmDialog>
    <ConfirmDialog open={showProof} title="补做同批次校样" onCancel={() => setShowProof(false)} onConfirm={() => void captureProof()}>
      <p>将为 <b>{run.code}</b> 创建一条 captured 校样。请在校样页提交复核并由复核员接收后，才能再次放行。</p>
    </ConfirmDialog>
  </section>;
}

type cardProps = {
  rework: ReworkRecord;
  run: DomainRecord;
  reviewer: boolean;
  reloadKey: number;
  reload: () => Promise<void>;
  busy: boolean;
  setBusy: (v: boolean) => void;
  setError: (v: string) => void;
};

function ReworkCard({ rework, run, reviewer, reloadKey, reload, busy, setBusy, setError }: cardProps) {
  const [detail, setDetail] = useState<ReworkDetail | null>(null);
  const [showRelease, setShowRelease] = useState(false);

  useEffect(() => {
    let active = true;
    void getRework(rework.id).then((res) => { if (active) setDetail(res.data); }).catch(() => undefined);
    return () => { active = false; };
  }, [rework.id, reloadKey, rework.status]);

  const reRelease = async () => {
    setBusy(true); setError('');
    try {
      await transitionPrintRun(run.id, 'released', run.version, '补做校样通过复核，批次再次放行');
      setShowRelease(false);
      await reload();
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  };

  const invalidated = detail?.invalidatedProofs ?? [];
  return <article className={`rework-card rework-card--${rework.status}`}>
    <header>
      <strong>{rework.code} · 第 {rework.reworkCount} 次返修</strong>
      <span className={`status status--${rework.status === 'waiting' ? 'warning' : 'success'}`}>
        {rework.status === 'waiting' ? '等待补做校样' : '已再次放行'}
      </span>
    </header>
    <dl>
      <div><dt>返修原因</dt><dd>{rework.reason}</dd></div>
      <div><dt>开始时间</dt><dd>{formatDate(rework.startedAt)}</dd></div>
      <div><dt>完成时间</dt><dd>{rework.completedAt ? formatDate(rework.completedAt) : '-'}</dd></div>
      {rework.resolutionProofCode && <div><dt>放行依据校样</dt><dd>{rework.resolutionProofCode}</dd></div>}
    </dl>
    {invalidated.length > 0 && <div className="rework-proofs">
      <h4>失效校样</h4>
      <ul>{invalidated.map((p) => <li key={p.id}><code>{p.code}</code><span className="status status--danger">已失效</span></li>)}</ul>
    </div>}
    {rework.status === 'waiting' && <div className="rework-gate">
      <p><strong>再次放行条件：</strong>为同一批次（{rework.runCode}）补做校样并通过质量复核后，批次才可再次放行。</p>
      {detail?.acceptedProof
        ? <p className="rework-ready">✓ 补做校样 <code>{detail.acceptedProof.code}</code> 已通过复核，可以再次放行。</p>
        : <p className="rework-pending">尚未检测到返修开始后通过复核的同批次校样。</p>}
      {reviewer && <UiButton onClick={() => setShowRelease(true)} disabled={busy || !detail?.releaseReady}>再次放行</UiButton>}
    </div>}
    <ConfirmDialog open={showRelease} title="确认批次再次放行" onCancel={() => setShowRelease(false)} onConfirm={() => void reRelease()}>
      <p>依据补做校样 <b>{detail?.acceptedProof?.code}</b> 将批次恢复为已放行并关闭本次返修。</p>
    </ConfirmDialog>
  </article>;
}
