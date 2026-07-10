import { useState, useEffect, useRef } from 'react';
import { useToast } from '../components/Toast';
import { listAccessories, type Accessory } from '../api/accessory';
import { outbound, batchOutbound, previewFileOutbound, executeFileOutbound, type OutboundCmd, type FileOutboundPreview, type FileForceOutboundResult } from '../api/stock';
import { scan } from '../api/replenishment';

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
const btnDanger: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #ff4d4f', background: '#ff4d4f', color: '#fff',
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

export default function Outbound() {
  const { showToast } = useToast();
  const [accessories, setAccessories] = useState<Accessory[]>([]);
  const [mode, setMode] = useState<'single' | 'batch' | 'file'>('file');

  // ── single mode ──
  const [sAccId, setSAccId] = useState<number | ''>('');
  const [sQty, setSQty] = useState(1);
  const [sPrice, setSPrice] = useState('');
  const [sRemark, setSRemark] = useState('');
  const [sClientRef, setSClientRef] = useState('');
  const [sResult, setSResult] = useState<{ id: number; balance_after: number } | null>(null);

  // ── batch mode ──
  interface BatchRow { key: number; accessory_id: number | ''; quantity: number; remark: string; }
  const [rows, setRows] = useState<BatchRow[]>([{ key: 1, accessory_id: '', quantity: 1, remark: '' }]);
  const [bResult, setBResult] = useState<{ accepted: number; flows: Array<{ id: number; balance_after: number }> } | null>(null);
  let nextRowKey = rows.length > 0 ? Math.max(...rows.map(r => r.key)) + 1 : 1;

  // ── file mode ──
  const fileRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<FileOutboundPreview | null>(null);
  const [showConfirm, setShowConfirm] = useState(false);
  const [parsing, setParsing] = useState(false);
  const [fResult, setFResult] = useState<FileForceOutboundResult | null>(null);

  const [submitting, setSubmitting] = useState(false);

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

  const handleSingleSubmit = async () => {
    if (sAccId === '') { showToast('error', '请选择配件'); return; }
    if (sQty <= 0) { showToast('error', '数量必须大于 0'); return; }
    setSubmitting(true);
    setSResult(null);
    try {
      const cmd: OutboundCmd = {
        accessory_id: Number(sAccId),
        quantity: sQty,
        unit_price: sPrice ? Number(sPrice) : undefined,
        remark: sRemark || undefined,
        client_ref: sClientRef || undefined,
      };
      const flow = await outbound(cmd);
      setSResult({ id: flow.id, balance_after: flow.balance_after });
      showToast('success', `出库成功 (流水 #${flow.id})`);
      setSAccId(''); setSQty(1); setSPrice(''); setSRemark(''); setSClientRef('');
    } catch (err: any) {
      const code = err?.error?.code;
      const message = err?.error?.message || '出库失败';
      if (code === 'INSUFFICIENT_STOCK') {
        showToast('error', `库存不足: ${message}`);
      } else {
        showToast('error', message);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleBatchSubmit = async () => {
    const invalid = rows.some(r => r.accessory_id === '' || r.quantity <= 0);
    if (invalid) { showToast('error', '请完善所有行（选择配件且数量 > 0）'); return; }
    setSubmitting(true);
    setBResult(null);
    try {
      const items: OutboundCmd[] = rows.map(r => ({
        accessory_id: Number(r.accessory_id),
        quantity: r.quantity,
        remark: r.remark || undefined,
      }));
      const res = await batchOutbound(items);
      setBResult({ accepted: res.accepted, flows: res.flows as any });
      showToast('success', `批量出库成功，共 ${res.accepted} 笔`);
    } catch (err: any) {
      const message = err?.error?.message || '批量出库失败';
      if (err?.error?.code === 'INSUFFICIENT_STOCK') {
        showToast('error', `库存不足: ${message}`);
      } else {
        showToast('error', message);
      }
    } finally {
      setSubmitting(false);
    }
  };

  // ── file mode handlers ──

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (f) setFile(f);
  };

  const handleParse = async () => {
    if (!file) { showToast('error', '请先选择 xlsx 文件'); return; }
    setParsing(true);
    setPreview(null);
    setFResult(null);
    try {
      const p = await previewFileOutbound(file);
      setPreview(p);
      setShowConfirm(true);
    } catch (err: any) {
      showToast('error', err?.error?.message || '解析文件失败');
    } finally {
      setParsing(false);
    }
  };

  const handleFileOutboundConfirm = async () => {
    if (!file) return;
    setShowConfirm(false);
    setSubmitting(true);
    setFResult(null);
    try {
      const res = await executeFileOutbound(file);
      setFResult(res);
      const parts: string[] = [`文件出库成功，${res.outbound} 笔`];
      if (res.created > 0) parts.push(`新建 ${res.created} 种`);
      if (res.shortages > 0) parts.push(`${res.shortages} 种库存不足已标记`);
      showToast('success', parts.join('，'));

      // Check shortage after outbound
      try {
        const scanRes = await scan();
        const shortItems = scanRes.items.filter(i => i.shortage > 0);
        if (shortItems.length > 0) {
          const names = shortItems.slice(0, 5).map(i => i.name).join('、');
          const more = shortItems.length > 5 ? ` 等${shortItems.length}个` : '';
          showToast('warning', `⚠️ ${shortItems.length} 个配件库存告急: ${names}${more}`);
        }
      } catch { /* shortage check is best-effort */ }

      setFile(null);
      setPreview(null);
      if (fileRef.current) fileRef.current.value = '';
    } catch (err: any) {
      showToast('error', err?.error?.message || '文件出库失败');
    } finally {
      setSubmitting(false);
    }
  };

  const resultBlock = (label: string, flowId: number, balance: number) => (
    <div style={{ marginTop: 12, padding: '8px 12px', background: '#fff7e6', border: '1px solid #ffd591', borderRadius: 4, fontSize: 13 }}>
      {label} 流水 ID: <strong>{flowId}</strong>，结余: <strong>{balance}</strong>
    </div>
  );

  return (
    <div>
      <h2 style={{ margin: '0 0 12px' }}>出库</h2>
      <div style={{ marginBottom: 12, display: 'flex', gap: 8 }}>
        <button style={mode === 'file' ? btn : btnGray} onClick={() => setMode('file')}>文件出库</button>
        <button style={mode === 'single' ? btn : btnGray} onClick={() => setMode('single')}>单笔出库</button>
        <button style={mode === 'batch' ? btn : btnGray} onClick={() => setMode('batch')}>批量出库</button>
      </div>

      {mode === 'single' && (
        <div style={{ maxWidth: 400 }}>
          <Field label="配件 *">
            <select style={inp} value={sAccId} onChange={e => setSAccId(e.target.value ? Number(e.target.value) : '')}>
              <option value="">-- 请选择 --</option>
              {accessories.map(a => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          </Field>
          <Field label="数量 *">
            <input style={inp} type="number" min={1} value={sQty} onChange={e => setSQty(Math.max(1, Number(e.target.value)))} />
          </Field>
          <Field label="单价（售价）">
            <input style={inp} type="number" min={0} step={0.01} value={sPrice} onChange={e => setSPrice(e.target.value)} />
          </Field>
          <Field label="客户参考号">
            <input style={inp} value={sClientRef} onChange={e => setSClientRef(e.target.value)} />
          </Field>
          <Field label="备注">
            <textarea style={{ ...inp, resize: 'vertical', minHeight: 48 }} value={sRemark} onChange={e => setSRemark(e.target.value)} />
          </Field>
          <button style={btn} disabled={submitting} onClick={handleSingleSubmit}>
            {submitting ? '提交中…' : '提交出库'}
          </button>
          {sResult && resultBlock('出库成功', sResult.id, sResult.balance_after)}
        </div>
      )}

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
                    <select style={{ ...inp, width: 200 }} value={r.accessory_id} onChange={e => updateRow(r.key, { accessory_id: e.target.value ? Number(e.target.value) : '' })}>
                      <option value="">-- 请选择 --</option>
                      {accessories.map(a => (
                        <option key={a.id} value={a.id}>{a.name}</option>
                      ))}
                    </select>
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
              {submitting ? '提交中…' : '提交批量出库'}
            </button>
          </div>
          {bResult && (
            <div style={{ marginTop: 12 }}>
              <div style={{ padding: '8px 12px', background: '#fff7e6', border: '1px solid #ffd591', borderRadius: 4, fontSize: 13, marginBottom: 8 }}>
                批量出库成功，共 {bResult.accepted} 笔
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

      {mode === 'file' && (
        <div style={{ maxWidth: 600 }}>
          <p style={{ fontSize: 13, color: '#666', marginBottom: 12 }}>
            上传 xlsx 发货单，系统自动解析"汇总"sheet 中的配件名称和数量，匹配后批量出库。
          </p>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 12 }}>
            <input ref={fileRef} type="file" accept=".xlsx" onChange={handleFileChange}
              style={{ fontSize: 13 }} />
            <button style={btn} disabled={!file || parsing} onClick={handleParse}>
              {parsing ? '解析中…' : '解析文件'}
            </button>
          </div>

          {fResult && (
            <div style={{ padding: '8px 12px', background: '#f6ffed', border: '1px solid #b7eb8f', borderRadius: 4, fontSize: 13 }}>
              文件出库成功，共 {fResult.outbound} 笔
              {fResult.created > 0 && <span>，新建 {fResult.created} 种配件</span>}
              {fResult.shortages > 0 && <span style={{ color: '#faad14' }}>，{fResult.shortages} 种库存不足已标记</span>}
            </div>
          )}

          {/* Confirmation Modal */}
          {showConfirm && preview && (
            <div style={{
              position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)',
              display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000,
            }}>
              <div style={{
                background: '#fff', padding: 24, borderRadius: 8,
                minWidth: 500, maxWidth: 700, maxHeight: '80vh', overflowY: 'auto',
              }}>
                <h3 style={{ margin: '0 0 12px' }}>确认文件出库</h3>
                <div style={{ fontSize: 13, color: '#666', marginBottom: 12 }}>
                  共 {preview.total_items} 种配件，{(preview.items || []).reduce((s, i) => s + i.quantity, 0) + (preview.not_found || []).reduce((s, n) => s + n.quantity, 0)} 件
                  {preview.not_found_count > 0 && (
                    <span style={{ color: '#1890ff' }}>（其中 {preview.not_found_count} 种将自动新建）</span>
                  )}
                </div>

                <table style={{ width: '100%', borderCollapse: 'collapse', marginBottom: 12 }}>
                  <thead>
                    <tr>
                      <th style={thS}>配件名称</th>
                      <th style={thS}>出库数量</th>
                      <th style={thS}>状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(preview.items || []).slice(0, 30).map((it, i) => (
                      <tr key={i} style={{ background: i % 2 === 0 ? '#f9f9f9' : '#fff' }}>
                        <td style={tdS}>{it.name}</td>
                        <td style={tdS}>{it.quantity}</td>
                        <td style={tdS}>
                          {it.current_stock < it.quantity
                            ? <span style={{ color: '#faad14', fontSize: 12 }}>⚠️ 缺{it.quantity - it.current_stock}（库存→0，阈值+{it.quantity - it.current_stock}）</span>
                            : <span style={{ color: '#52c41a', fontSize: 12 }}>✅ 库存充足</span>
                          }
                        </td>
                      </tr>
                    ))}
                    {(preview.items || []).length > 30 && (
                      <tr><td style={tdS} colSpan={3}>… 还有 {(preview.items || []).length - 30} 项</td></tr>
                    )}
                    {(preview.not_found || []).map((nf, i) => (
                      <tr key={`nf-${i}`} style={{ background: '#f0f5ff' }}>
                        <td style={tdS}>{nf.name}</td>
                        <td style={tdS}>{nf.quantity}</td>
                        <td style={tdS}><span style={{ color: '#1890ff', fontSize: 12 }}>🆕 自动新建</span></td>
                      </tr>
                    ))}
                  </tbody>
                </table>

                <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                  <button style={btnGray} onClick={() => setShowConfirm(false)}>取消</button>
                  <button style={btnDanger} disabled={submitting} onClick={handleFileOutboundConfirm}>
                    {submitting ? '出库中…' : `确认出库`}
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}