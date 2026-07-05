import { useState, useEffect } from 'react';
import { useToast } from '../components/Toast';
import { listAccessories, type Accessory } from '../api/accessory';
import { listFlows, type FlowListParams } from '../api/flow';
import type { InventoryFlow } from '../api/stock';

const thS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', background: '#fafafa',
  textAlign: 'left', fontWeight: 600, fontSize: 13,
};
const tdS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', fontSize: 13,
};
const inp: React.CSSProperties = {
  padding: '6px 10px', border: '1px solid #d9d9d9', borderRadius: 4,
  fontSize: 13, boxSizing: 'border-box' as const,
};
const btn: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #1890ff', background: '#1890ff',
  color: '#fff',
};
const btnGray: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #d9d9d9', background: '#fff',
  color: '#333',
};

function fmtDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleString('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

export default function Flows() {
  const { showToast } = useToast();
  const [accessories, setAccessories] = useState<Accessory[]>([]);
  const [accMap, setAccMap] = useState<Record<number, string>>({});
  const [items, setItems] = useState<InventoryFlow[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  // filters
  const [filterAccId, setFilterAccId] = useState<number | ''>('');
  const [filterType, setFilterType] = useState<'all' | 'in' | 'out'>('all');
  const [filterFrom, setFilterFrom] = useState('');
  const [filterTo, setFilterTo] = useState('');

  const [offset, setOffset] = useState(0);
  const limit = 15;

  useEffect(() => {
    listAccessories(undefined, 1000, 0)
      .then(res => {
        setAccessories(res.items);
        const m: Record<number, string> = {};
        res.items.forEach(a => { m[a.id] = a.name; });
        setAccMap(m);
      })
      .catch(err => showToast('error', err?.error?.message || '加载配件列表失败'));
  }, [showToast]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const params: FlowListParams = { limit, offset };
    if (filterAccId !== '') params.accessory_id = Number(filterAccId);
    if (filterType !== 'all') params.type = filterType;
    if (filterFrom) params.from = filterFrom;
    if (filterTo) params.to = filterTo;
    listFlows(params)
      .then(res => {
        if (cancelled) return;
        setItems(res.items);
        setTotal(res.total);
      })
      .catch(err => {
        if (cancelled) return;
        showToast('error', err?.error?.message || '加载流水失败');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [filterAccId, filterType, filterFrom, filterTo, offset, limit, showToast]);

  const handleFilter = () => { setOffset(0); };

  const totalPages = Math.ceil(total / limit);
  const currentPage = totalPages > 0 ? Math.floor(offset / limit) + 1 : 0;

  return (
    <div>
      <h2 style={{ margin: '0 0 12px' }}>流水</h2>

      {/* filters */}
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'flex-end', marginBottom: 12 }}>
        <div>
          <label style={{ display: 'block', fontSize: 12, marginBottom: 2 }}>配件</label>
          <select style={{ ...inp, width: 160 }} value={filterAccId} onChange={e => setFilterAccId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">全部</option>
            {accessories.map(a => (
              <option key={a.id} value={a.id}>{a.sku} - {a.name}</option>
            ))}
          </select>
        </div>
        <div>
          <label style={{ display: 'block', fontSize: 12, marginBottom: 2 }}>类型</label>
          <select style={inp} value={filterType} onChange={e => setFilterType(e.target.value as 'all' | 'in' | 'out')}>
            <option value="all">全部</option>
            <option value="in">入库</option>
            <option value="out">出库</option>
          </select>
        </div>
        <div>
          <label style={{ display: 'block', fontSize: 12, marginBottom: 2 }}>起始</label>
          <input type="date" style={inp} value={filterFrom} onChange={e => setFilterFrom(e.target.value)} />
        </div>
        <div>
          <label style={{ display: 'block', fontSize: 12, marginBottom: 2 }}>截止</label>
          <input type="date" style={inp} value={filterTo} onChange={e => setFilterTo(e.target.value)} />
        </div>
        <button style={btn} onClick={handleFilter}>查询</button>
      </div>

      {/* table */}
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={thS}>时间</th>
            <th style={thS}>配件</th>
            <th style={thS}>类型</th>
            <th style={thS}>数量</th>
            <th style={thS}>单价</th>
            <th style={thS}>结余</th>
            <th style={thS}>备注</th>
          </tr>
        </thead>
        <tbody>
          {loading && (
            <tr><td style={tdS} colSpan={7}>加载中…</td></tr>
          )}
          {!loading && items.length === 0 && (
            <tr><td style={tdS} colSpan={7}>暂无数据</td></tr>
          )}
          {!loading && items.map((f, i) => (
            <tr key={f.id} style={{ background: i % 2 === 0 ? '#f9f9f9' : '#fff' }}>
              <td style={tdS}>{fmtDate(f.occurred_at)}</td>
              <td style={tdS}>{accMap[f.accessory_id] || `#${f.accessory_id}`}</td>
              <td style={{ ...tdS, color: f.type === 'in' ? '#52c41a' : '#ff4d4f' }}>
                {f.type === 'in' ? '入库' : '出库'}
              </td>
              <td style={tdS}>{f.quantity}</td>
              <td style={tdS}>
                {f.type === 'in'
                  ? (f.unit_cost ? `¥${f.unit_cost}` : '-')
                  : (f.unit_price ? `¥${f.unit_price}` : '-')
                }
              </td>
              <td style={{ ...tdS, fontWeight: 600 }}>{f.balance_after}</td>
              <td style={tdS}>{f.remark || '-'}</td>
            </tr>
          ))}
        </tbody>
      </table>

      {/* pagination */}
      {totalPages > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 12, fontSize: 13 }}>
          <button style={btnGray} disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>上一页</button>
          <span>第 {currentPage} / {totalPages} 页（共 {total} 条）</span>
          <button style={btnGray} disabled={offset + limit >= total} onClick={() => setOffset(offset + limit)}>下一页</button>
        </div>
      )}
    </div>
  );
}
