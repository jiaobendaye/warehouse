import { useState, useEffect, type ReactNode } from 'react';
import { useToast } from '../components/Toast';
import {
  listAccessories,
  createAccessory,
  updateAccessory,
  deleteAccessory,
  type Accessory,
} from '../api/accessory';

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
  const [offset, setOffset] = useState(0);
  const limit = 10;
  const [refreshKey, setRefreshKey] = useState(0);

  // ── create modal state ──
  const [showCreate, setShowCreate] = useState(false);
  const [cSku, setCSku] = useState('');
  const [cName, setCName] = useState('');
  const [cThreshold, setCThreshold] = useState(0);
  const [cNotes, setCNotes] = useState('');
  const [cErrors, setCErrors] = useState<Record<string, string>>({});
  const [cSubmitting, setCSubmitting] = useState(false);

  // ── edit modal state ──
  const [editItem, setEditItem] = useState<Accessory | null>(null);
  const [eName, setEName] = useState('');
  const [eThreshold, setEThreshold] = useState(0);
  const [eNotes, setENotes] = useState('');
  const [eErrors, setEErrors] = useState<Record<string, string>>({});
  const [eSubmitting, setESubmitting] = useState(false);

  // ── data fetch ──
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listAccessories(q || undefined, limit, offset)
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
  }, [q, offset, limit, refreshKey, showToast]);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setQ(searchInput);
    setOffset(0);
  };

  const totalPages = Math.ceil(total / limit);
  const currentPage = totalPages > 0 ? Math.floor(offset / limit) + 1 : 0;

  // ── create helpers ──
  const resetCreateForm = () => {
    setCSku(''); setCName(''); setCThreshold(0);
    setCNotes(''); setCErrors({});
  };

  const validateCreate = (): boolean => {
    const errs: Record<string, string> = {};
    if (!cSku.trim()) errs.sku = 'SKU 不能为空';
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
        sku: cSku.trim(),
        name: cName.trim(),
        low_stock_threshold: cThreshold,
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
  const handleDelete = async (item: Accessory) => {
    if (!window.confirm(`确定要删除配件 "${item.name}" (${item.sku}) 吗？`)) return;
    try {
      await deleteAccessory(item.id);
      showToast('success', '配件已删除');
      setRefreshKey(k => k + 1);
    } catch (err: any) {
      showToast('error', err?.error?.message || '删除失败');
    }
  };

  // ── modal sections ──
  const createModal = showCreate && (
    <Modal title="新建配件" onClose={() => { setShowCreate(false); resetCreateForm(); }}>
      <Field label="SKU *" error={cErrors.sku}>
        <input style={inp} value={cSku} onChange={e => setCSku(e.target.value)} />
      </Field>
      <Field label="名称 *" error={cErrors.name}>
        <input style={inp} value={cName} onChange={e => setCName(e.target.value)} />
      </Field>
      <Field label="低库存阈值" error={cErrors.threshold}>
        <input style={inp} type="number" min={0} value={cThreshold} onChange={e => setCThreshold(Number(e.target.value))} />
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
    <Modal title={`编辑配件 - ${editItem.sku}`} onClose={() => setEditItem(null)}>
      <div style={{ ...fieldS }}>
        <label style={labelS}>SKU</label>
        <div style={{ padding: '6px 10px', fontSize: 13, color: '#888' }}>{editItem.sku}</div>
      </div>
      <Field label="名称 *" error={eErrors.name}>
        <input style={inp} value={eName} onChange={e => setEName(e.target.value)} />
      </Field>
      <Field label="低库存阈值" error={eErrors.threshold}>
        <input style={inp} type="number" min={0} value={eThreshold} onChange={e => setEThreshold(Number(e.target.value))} />
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
            placeholder="搜索 SKU 或名称…"
            value={searchInput}
            onChange={e => setSearchInput(e.target.value)}
          />
          <button style={btn} type="submit">搜索</button>
        </form>
        <button style={btn} onClick={() => setShowCreate(true)}>新建配件</button>
      </div>

      {/* table */}
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={thS}>SKU</th>
            <th style={thS}>名称</th>
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
              <td style={tdS}>{item.sku}</td>
              <td style={tdS}>{item.name}</td>
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
    </div>
  );
}
