import { useState, useEffect, useRef } from 'react';
import { useToast } from '../components/Toast';
import AccessorySelect from '../components/AccessorySelect';
import { listAccessories, type Accessory } from '../api/accessory';
import { inbound, batchInbound, executeFileInbound, type InboundCmd, type FileInboundResult } from '../api/stock';

// ── shared styles ──
const inp: React.CSSProperties = {
  padding: '6px 10px', border: '1px solid #d9d9d9', borderRadius: 4,
  fontSize: 13, boxSizing: 'border-box' as const, width: '100%',
};
const btn: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #1890ff', background: '#1890ff',
  color: '#fff',
};
const btnGray: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #d9d9d9', background: '#fff', color: '#333',
};
const tdS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', fontSize: 13,
};
const thS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', background: '#fafafa',
  textAlign: 'left', fontWeight: 600, fontSize: 13,
};
const labelS: React.CSSProperties = { display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500 };
const fieldS: React.CSSProperties = { marginBottom: 12 };
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div style={fieldS}><label style={labelS}>{label}</label>{children}</div>;
}

export default function Inbound() {
  const { showToast } = useToast();
  const [accessories, setAccessories] = useState<Accessory[]>([]);
  const [mode, setMode] = useState<'file' | 'single' | 'batch'>('file');

  // ── single mode ──
  const [sAccId, setSAccId] = useState<number | ''>('');
  const [sQty, setSQty] = useState(1);
  const [sCost, setSCost] = useState('');
  const [sRemark, setSRemark] = useState('');
  const [sClientRef, setSClientRef] = useState('');
  const [sResult, setSResult] = useState<{ id: number; balance_after: number } | null>(null);

  // ── batch mode ──
  interface BatchRow { key: number; accessory_id: number | ''; quantity: number; remark: string; }
  const [rows, setRows] = useState<BatchRow[]>([{ key: 1, accessory_id: '', quantity: 1, remark: '' }]);
  const [bResult, setBResult] = useState<{ accepted: number; flows: Array<{ id: number; balance_after: number }> } | null>(null);

  // ── file mode ──
  const fileRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [fResult, setFResult] = useState<FileInboundResult | null>(null);

  const [submitting, setSubmitting] = useState(false);
  let nextRowKey = rows.length > 0 ? Math.max(...rows.map(r => r.key)) + 1 : 1;

  useEffect(() => {
    if (mode === 'file') return;
    if (accessories.length > 0) return;
    listAccessories(undefined, 1000, 0)
      .then(res => setAccessories(res.items))
      .catch(err => showToast('error', err?.error?.message || '加载配件列表失败'));
  }, [mode, showToast, accessories.length]);

  const updateRow = (key: number, patch: Partial<BatchRow>) => {
    setRows(prev => prev.map(r => r.key === key ? { ...r, ...patch } : r));
  };
  const addRow = () => {
    setRows(prev => [...prev, { key: nextRowKey, accessory_id: '', quantity: 1, remark: '' }]);
    nextRowKey++;
  };
  const removeRow = (key: number) => {
    setRows(prev => prev.length > 1 ? prev.filter(r => r.key !== key) : prev);
  };

  // ── single submit ──
  const handleSingleSubmit = async () => {
    if (sAccId === '') { showToast('error', '请选择配件'); return; }
    if (sQty <= 0) { showToast('error', '数量必须大于 0'); return; }
    setSubmitting(true);
    setSResult(null);
    try {
      const cmd: InboundCmd = {
        accessory_id: Number(sAccId),
        quantity: sQty,
        unit_cost: sCost ? Number(sCost) : undefined,
        remark: sRemark || undefined,
        client_ref: sClientRef || undefined,
      };
      const flow = await inbound(cmd);
      setSResult({ id: flow.id, balance_after: flow.balance_after });
      showToast('success', `入库成功 (流水 #${flow.id})`);
      // reset
      setSAccId(''); setSQty(1); setSCost(''); setSRemark(''); setSClientRef('');
    } catch (err: any) {
      showToast('error', err?.error?.message || '入库失败');
    } finally {
      setSubmitting(false);
    }
  };

  // ── batch submit ──
  const handleBatchSubmit = async () => {
    const invalid = rows.some(r => r.accessory_id === '' || r.quantity <= 0);
    if (invalid) { showToast('error', '请完善所有行（选择配件且数量 > 0）'); return; }
    setSubmitting(true);
    setBResult(null);
    try {
      const items: InboundCmd[] = rows.map(r => ({
        accessory_id: Number(r.accessory_id),
        quantity: r.quantity,
        remark: r.remark || undefined,
      }));
      const res = await batchInbound(items);
      setBResult({ accepted: res.accepted, flows: res.flows as any });
      showToast('success', `批量入库成功，共 ${res.accepted} 笔`);
    } catch (err: any) {
      showToast('error', err?.error?.message || '批量入库失败');
    } finally {
      setSubmitting(false);
    }
  };

  // ── result display ──
  const resultBlock = (label: string, flowId: number, balance: number) => (
    <div style={{ marginTop: 12, padding: '8px 12px', background: '#f6ffed', border: '1px solid #b7eb8f', borderRadius: 4, fontSize: 13 }}>
      {label} 流水 ID: <strong>{flowId}</strong>，结余: <strong>{balance}</strong>
    </div>
  );

  // ── file mode handlers ──

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (f) setFile(f);
  };

  const handleFileInbound = async () => {
    if (!file) { showToast('error', '请先选择 xlsx 文件'); return; }
    setSubmitting(true);
    setFResult(null);
    try {
      const res = await executeFileInbound(file);
      setFResult(res);
      const parts: string[] = [`文件入库成功，${res.inbound} 笔`];
      if (res.created > 0) parts.push(`新建 ${res.created} 种`);
      showToast('success', parts.join('，'));
      setFile(null);
      if (fileRef.current) fileRef.current.value = '';
    } catch (err: any) {
      showToast('error', err?.error?.message || '文件入库失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <h2 style={{ margin: '0 0 12px' }}>入库</h2>

      {/* mode toggle */}
      <div style={{ marginBottom: 12, display: 'flex', gap: 8 }}>
        <button style={mode === 'file' ? btn : btnGray} onClick={() => setMode('file')}>文件入库</button>
        <button style={mode === 'single' ? btn : btnGray} onClick={() => setMode('single')}>单笔入库</button>
        <button style={mode === 'batch' ? btn : btnGray} onClick={() => setMode('batch')}>批量入库</button>
      </div>

      {/* single form */}
      {mode === 'single' && (
        <div style={{ maxWidth: 400 }}>
          <Field label="配件 *">
            <AccessorySelect accessories={accessories} value={sAccId} onChange={setSAccId} />
          </Field>
          <Field label="数量 *">
            <input style={inp} type="number" min={1} value={sQty} onChange={e => setSQty(Math.max(1, Number(e.target.value)))} />
          </Field>
          <Field label="单价（成本价）">
            <input style={inp} type="number" min={0} step={0.01} value={sCost} onChange={e => setSCost(e.target.value)} />
          </Field>
          <Field label="客户参考号">
            <input style={inp} value={sClientRef} onChange={e => setSClientRef(e.target.value)} />
          </Field>
          <Field label="备注">
            <textarea style={{ ...inp, resize: 'vertical', minHeight: 48 }} value={sRemark} onChange={e => setSRemark(e.target.value)} />
          </Field>
          <button style={btn} disabled={submitting} onClick={handleSingleSubmit}>
            {submitting ? '提交中…' : '提交入库'}
          </button>
          {sResult && resultBlock('入库成功', sResult.id, sResult.balance_after)}
        </div>
      )}

      {/* batch form */}
      {mode === 'batch' && (
        <div>
          <table style={{ borderCollapse: 'collapse', width: '100%', marginBottom: 12 }}>
            <thead>
              <tr>
                <th style={thS}>配件 *</th>
                <th style={thS}>数量 *</th>
                <th style={thS}>备注</th>
                <th style={thS}>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={r.key} style={{ background: i % 2 === 0 ? '#f9f9f9' : '#fff' }}>
                  <td style={tdS}>
                    <AccessorySelect accessories={accessories} value={r.accessory_id} onChange={v => updateRow(r.key, { accessory_id: v })} width={200} />
                  </td>
                  <td style={tdS}>
                    <input style={{ ...inp, width: 80 }} type="number" min={1} value={r.quantity} onChange={e => updateRow(r.key, { quantity: Math.max(1, Number(e.target.value)) })} />
                  </td>
                  <td style={tdS}>
                    <input style={{ ...inp, width: 160 }} value={r.remark} onChange={e => updateRow(r.key, { remark: e.target.value })} />
                  </td>
                  <td style={tdS}>
                    <button style={btnGray} onClick={() => removeRow(r.key)} disabled={rows.length <= 1}>删除</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
            <button style={btnGray} onClick={addRow}>添加行</button>
            <button style={btn} disabled={submitting} onClick={handleBatchSubmit}>
              {submitting ? '提交中…' : '提交批量入库'}
            </button>
          </div>
          {bResult && (
            <div style={{ marginTop: 12 }}>
              <div style={{ padding: '8px 12px', background: '#f6ffed', border: '1px solid #b7eb8f', borderRadius: 4, fontSize: 13, marginBottom: 8 }}>
                批量入库成功，共 {bResult.accepted} 笔
              </div>
              {bResult.flows.map(f => (
                <div key={f.id} style={{ padding: '4px 12px', fontSize: 13, color: '#555' }}>
                  流水 #{f.id} 结余: {f.balance_after}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* file mode */}
      {mode === 'file' && (
        <div style={{ maxWidth: 600 }}>
          <p style={{ fontSize: 13, color: '#666', marginBottom: 12 }}>
            上传 xlsx 入库单，读取第一个 sheet（首行表头 + [配件, 数量] 数据行），系统自动入库并在配件不存在时新建。
          </p>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 12 }}>
            <input ref={fileRef} type="file" accept=".xlsx" onChange={handleFileChange}
              style={{ fontSize: 13 }} />
            <button style={btn} disabled={!file || submitting} onClick={handleFileInbound}>
              {submitting ? '入库中…' : '确认入库'}
            </button>
          </div>

          {fResult && (
            <div style={{ padding: '8px 12px', background: '#f6ffed', border: '1px solid #b7eb8f', borderRadius: 4, fontSize: 13 }}>
              文件入库成功，共 {fResult.inbound} 笔
              {fResult.created > 0 && <span>，新建 {fResult.created} 种配件</span>}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
