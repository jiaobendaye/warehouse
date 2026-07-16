import { useState, useEffect, type ReactNode } from 'react';
import { useToast } from '../components/Toast';
import { isWails } from '../api/client';
import {
  listAccessories,
  listStalls,
  createAccessory,
  updateAccessory,
  deleteAccessory,
  exportAccessories,
  type Accessory,
} from '../api/accessory';
import { listFlows, type FlowListResponse } from '../api/flow';

// ── shared styles ──────────────────────────────────────────────
const thS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', background: '#fafafa',
  textAlign: 'left', fontWeight: 600, fontSize: 13,
};
const tdS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', fontSize: 13,
};
const btn: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #1890ff', background: '#1890ff',
  color: '#fff',
};
const btnDanger: React.CSSProperties = {
  ...btn, background: '#ff4d4f', borderColor: '#ff4d4f',
};
const btnGray: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #d9d9d9', background: '#fff',
  color: '#333',
};
const inp: React.CSSProperties = {
  padding: '6px 10px', border: '1px solid #d9d9d9', borderRadius: 4,
  fontSize: 13, boxSizing: 'border-box' as const, width: '100%',
};
const labelS: React.CSSProperties = {
  display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500,
};
const fieldS: React.CSSProperties = { marginBottom: 12 };
const errS: React.CSSProperties = { color: '#ff4d4f', fontSize: 12, marginTop: 2 };

// ── Modal overlay ──────────────────────────────────────────────
function Modal({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  return (
    <div style={{
      position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      zIndex: 1000,
    }} onClick={onClose}>
      <div style={{
        background: '#fff', padding: 24, borderRadius: 8,
        minWidth: 380, maxWidth: 500, maxHeight: '80vh', overflowY: 'auto',
      }} onClick={e => e.stopPropagation()}>
        <h3 style={{ margin: '0 0 16px' }}>{title}</h3>
        {children}
      </div>
    </div>
  );
}

// ── Field helper ──────────────────────────────────────────────
function Field({ label, error, children }: { label: string; error?: string; children: ReactNode }) {
  return (
    <div style={fieldS}>
      <label style={labelS}>{label}</label>
      {children}
      {error && <div style={errS}>{error}</div>}
    </div>
  );
}

// ── Page component ─────────────────────────────────────────────
export default function AccessoryList() {
  const { showToast } = useToast();
  const [items, setItems] = useState<Accessory[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [q, setQ] = useState('');
  const [searchInput, setSearchInput] = useState('');
  const [stallFilter, setStallFilter] = useState('');
  const [stalls, setStalls] = useState<string[]>([]);
  const [offset, setOffset] = useState(0);
  const limit = 10;
  const [refreshKey, setRefreshKey] = useState(0);

  // ── create modal state ──
  const [showCreate, setShowCreate] = useState(false);
  const [cName, setCName] = useState('');
  const [cThreshold, setCThreshold] = useState(0);
  const [cStall, setCStall] = useState('');
  const [cNotes, setCNotes] = useState('');
  const [cErrors, setCErrors] = useState<Record<string, string>>({});
  const [cSubmitting, setCSubmitting] = useState(false);

  // ── edit modal state ──
  const [editItem, setEditItem] = useState<Accessory | null>(null);
  const [eName, setEName] = useState('');
  const [eThreshold, setEThreshold] = useState(0);
  const [eStall, setEStall] = useState('');
  const [eNotes, setENotes] = useState('');
  const [eErrors, setEErrors] = useState<Record<string, string>>({});
  const [eSubmitting, setESubmitting] = useState(false);

  // ── fetch stalls for filter + autocomplete ──
  useEffect(() => {
    listStalls()
      .then(res => setStalls(res.stalls || []))
      .catch(() => {});
  }, [refreshKey]);

  // ── data fetch ──
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listAccessories(q || undefined, limit, offset, stallFilter || undefined)
      .then(res => {
        if (cancelled) return;
        setItems(res.items);
        setTotal(res.total);
      })
      .catch(err => {
        if (cancelled) return;
        showToast('error', err?.error?.message || '加载配件列表失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [q, stallFilter, offset, limit, refreshKey, showToast]);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setQ(searchInput);
    setOffset(0);
  };

  const handleStallFilter = (v: string) => {
    setStallFilter(v);
    setOffset(0);
  };

  const totalPages = Math.ceil(total / limit);
  const currentPage = totalPages > 0 ? Math.floor(offset / limit) + 1 : 0;

  // ── create helpers ──
  const resetCreateForm = () => {
    setCName(''); setCThreshold(0);
    setCStall(''); setCNotes(''); setCErrors({});
  };

  const validateCreate = (): boolean => {
    const errs: Record<string, string> = {};
    if (!cName.trim()) errs.name = '名称不能为空';
    if (cThreshold < 0) errs.threshold = '阈值不能小于 0';
    setCErrors(errs);
    return Object.keys(errs).length === 0;
  };

  const handleCreate = async () => {
    if (!validateCreate()) return;
    setCSubmitting(true);
    try {
      await createAccessory({
        name: cName.trim(),
        low_stock_threshold: cThreshold,
        stall: cStall.trim() || undefined,
        notes: cNotes.trim() || undefined,
      });
      showToast('success', '配件创建成功');
      setShowCreate(false);
      resetCreateForm();
      setOffset(0);
      setRefreshKey(k => k + 1);
    } catch (err: any) {
      showToast('error', err?.error?.message || '创建失败');
    } finally {
      setCSubmitting(false);
    }
  };

  // ── edit helpers ──
  const openEdit = (item: Accessory) => {
    setEditItem(item);
    setEName(item.name);
    setEThreshold(item.low_stock_threshold);
    setEStall(item.stall || '');
    setENotes(item.notes || '');
    setEErrors({});
  };

  const validateEdit = (): boolean => {
    const errs: Record<string, string> = {};
    if (!eName.trim()) errs.name = '名称不能为空';
    if (eThreshold < 0) errs.threshold = '阈值不能小于 0';
    setEErrors(errs);
    return Object.keys(errs).length === 0;
  };

  const handleEdit = async () => {
    if (!editItem || !validateEdit()) return;
    setESubmitting(true);
    try {
      await updateAccessory(editItem.id, {
        name: eName.trim(),
        low_stock_threshold: eThreshold,
        stall: eStall.trim() || undefined,
        notes: eNotes.trim() || undefined,
      });
      showToast('success', '配件更新成功');
      setEditItem(null);
      setRefreshKey(k => k + 1);
    } catch (err: any) {
      showToast('error', err?.error?.message || '更新失败');
    } finally {
      setESubmitting(false);
    }
  };

  // ── delete ──
  const [pendingDelete, setPendingDelete] = useState<Accessory | null>(null);
  const [flowCount, setFlowCount] = useState<number | null>(null);
  const [countLoading, setCountLoading] = useState(false);
  const [deleteSubmitting, setDeleteSubmitting] = useState(false);

  const handleDelete = (item: Accessory) => {
    setPendingDelete(item);
  };

  useEffect(() => {
    if (!pendingDelete) {
      setFlowCount(null);
      return;
    }
    let cancelled = false;
    setCountLoading(true);
    setFlowCount(null);
    listFlows({ accessory_id: pendingDelete.id })
      .then((res: FlowListResponse) => {
        if (!cancelled) setFlowCount(res.total);
      })
      .catch(() => {
        if (!cancelled) setFlowCount(0); // fall back to 0 on error so user can still try
      })
      .finally(() => {
        if (!cancelled) setCountLoading(false);
      });
    return () => { cancelled = true; };
  }, [pendingDelete]);

  const handleDeleteConfirm = async () => {
    if (!pendingDelete) return;
    setDeleteSubmitting(true);
    try {
      const res = await deleteAccessory(pendingDelete.id);
      showToast('success', `配件及 ${res.flows_deleted} 条流水已删除`);
      setPendingDelete(null);
      setRefreshKey(k => k + 1);
    } catch (err: any) {
      showToast('error', err?.error?.message || '删除失败');
    } finally {
      setDeleteSubmitting(false);
    }
  };

  // ── export ──
  // Hits the backend xlsx endpoint, gets a Blob, then triggers a hidden
  // <a download> click so the browser saves the file. Object URL is
  // revoked on next tick — well after the click — to keep it valid for
  // the download to start.
  const [exporting, setExporting] = useState(false);
  const handleExport = async () => {
    if (exporting) return;
    // GUI 内 WebView 不支持触发浏览器下载，提示用户在浏览器中导出
    if (isWails()) {
      showToast('warning', '请在浏览器中打开本应用后再导出文件');
      return;
    }
    setExporting(true);
    try {
      const blob = await exportAccessories();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `配件库存_${formatStamp(new Date())}.xlsx`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 0);
      showToast('success', '导出已开始');
    } catch (err: any) {
      showToast('error', err?.error?.message || '导出失败');
    } finally {
      setExporting(false);
    }
  };

  // ── modal sections ──
  const createModal = showCreate && (
    <Modal title="新建配件" onClose={() => { setShowCreate(false); resetCreateForm(); }}>
      <Field label="名称 *" error={cErrors.name}>
        <input style={inp} value={cName} onChange={e => setCName(e.target.value)} />
      </Field>
      <Field label="低库存阈值" error={cErrors.threshold}>
        <input style={inp} type="number" min={0} value={cThreshold} onChange={e => setCThreshold(Number(e.target.value))} />
      </Field>
      <Field label="档口">
        <input style={inp} list="stall-list" value={cStall} onChange={e => setCStall(e.target.value)} placeholder="未分配" />
      </Field>
      <Field label="备注">
        <textarea style={{ ...inp, resize: 'vertical' as const, minHeight: 48 }} value={cNotes} onChange={e => setCNotes(e.target.value)} />
      </Field>
      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
        <button style={btnGray} onClick={() => { setShowCreate(false); resetCreateForm(); }}>取消</button>
        <button style={btn} disabled={cSubmitting} onClick={handleCreate}>
          {cSubmitting ? '提交中…' : '创建'}
        </button>
      </div>
    </Modal>
  );

  const editModal = editItem && (
    <Modal title={`编辑配件 - ${editItem.name}`} onClose={() => setEditItem(null)}>
      <div style={{ ...fieldS }}>
        <label style={labelS}>ID</label>
        <div style={{ padding: '6px 10px', fontSize: 13, color: '#888' }}>{editItem.id}</div>
      </div>
      <Field label="名称 *" error={eErrors.name}>
        <input style={inp} value={eName} onChange={e => setEName(e.target.value)} />
      </Field>
      <Field label="低库存阈值" error={eErrors.threshold}>
        <input style={inp} type="number" min={0} value={eThreshold} onChange={e => setEThreshold(Number(e.target.value))} />
      </Field>
      <Field label="档口">
        <input style={inp} list="stall-list" value={eStall} onChange={e => setEStall(e.target.value)} placeholder="未分配" />
      </Field>
      <Field label="备注">
        <textarea style={{ ...inp, resize: 'vertical', minHeight: 48 }} value={eNotes} onChange={e => setENotes(e.target.value)} />
      </Field>
      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
        <button style={btnGray} onClick={() => setEditItem(null)}>取消</button>
        <button style={btn} disabled={eSubmitting} onClick={handleEdit}>
          {eSubmitting ? '提交中…' : '保存'}
        </button>
      </div>
    </Modal>
  );

  return (
    <div>
      <h2 style={{ margin: '0 0 12px' }}>配件列表</h2>

      {/* search + create */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <form onSubmit={handleSearch} style={{ display: 'flex', gap: 8 }}>
          <input
            style={{ ...inp, width: 240 }}
            placeholder="搜索名称…"
            value={searchInput}
            onChange={e => setSearchInput(e.target.value)}
          />
          <button style={btn} type="submit">搜索</button>
          <select
            style={{ ...inp, width: 140 }}
            value={stallFilter}
            onChange={e => handleStallFilter(e.target.value)}
          >
            <option value="">全部档口</option>
            {stalls.map(s => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
        </form>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btnGray} disabled={exporting} onClick={handleExport}>
            {exporting ? '导出中…' : '导出'}
          </button>
          <button style={btn} onClick={() => setShowCreate(true)}>新建配件</button>
        </div>
      </div>

      {/* table */}
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={thS}>名称</th>
            <th style={thS}>档口</th>
            <th style={thS}>当前库存</th>
            <th style={thS}>阈值</th>
            <th style={thS}>操作</th>
          </tr>
        </thead>
        <tbody>
          {loading && (
            <tr><td style={tdS} colSpan={5}>加载中…</td></tr>
          )}
          {!loading && items.length === 0 && (
            <tr><td style={tdS} colSpan={5}>暂无数据</td></tr>
          )}
          {!loading && items.map((item, i) => (
            <tr key={item.id} style={{ background: i % 2 === 0 ? '#f9f9f9' : '#fff' }}>
              <td style={tdS}>{item.name}</td>
              <td style={tdS}>{item.stall || '未分配'}</td>
              <td style={tdS}>{item.current_stock}</td>
              <td style={tdS}>{item.low_stock_threshold}</td>
              <td style={tdS}>
                <button style={{ ...btnGray, marginRight: 6 }} onClick={() => openEdit(item)}>编辑</button>
                <button style={btnDanger} onClick={() => handleDelete(item)}>删除</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {/* pagination */}
      {totalPages > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 12, fontSize: 13 }}>
          <button
            style={btnGray}
            disabled={offset === 0}
            onClick={() => setOffset(Math.max(0, offset - limit))}
          >
            上一页
          </button>
          <span>第 {currentPage} / {totalPages} 页（共 {total} 条）</span>
          <button
            style={btnGray}
            disabled={offset + limit >= total}
            onClick={() => setOffset(offset + limit)}
          >
            下一页
          </button>
        </div>
      )}

      {createModal}
      {editModal}
      {pendingDelete && (
        <Modal title="删除配件" onClose={() => setPendingDelete(null)}>
          <div style={{ fontSize: 13, lineHeight: 1.6 }}>
            <p style={{ margin: '0 0 12px 0' }}>
              确定要删除配件「<strong>{pendingDelete.name}</strong>」吗？
            </p>
            <p style={{ margin: 0, color: '#ff4d4f' }}>
              这将一并删除
              <strong>{flowCount === null ? ' …' : ` ${flowCount} `}</strong>
              条流水记录，此操作不可恢复。
            </p>
          </div>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
            <button
              style={btnGray}
              onClick={() => setPendingDelete(null)}
              disabled={deleteSubmitting}
            >
              取消
            </button>
            <button
              style={btnDanger}
              onClick={handleDeleteConfirm}
              disabled={deleteSubmitting || countLoading || flowCount === null}
            >
              {deleteSubmitting ? '删除中…' : '删除'}
            </button>
          </div>
        </Modal>
      )}
      <datalist id="stall-list">
        {stalls.map(s => <option key={s} value={s} />)}
      </datalist>
    </div>
  );
}

// formatStamp renders a Date as YYYYMMDD_HHMMSS in local time, mirroring
// the backend's filename timestamp so the two stay aligned. Used for the
// xlsx download filename — no timezone math, just local.
function formatStamp(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}` +
    `_${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
}