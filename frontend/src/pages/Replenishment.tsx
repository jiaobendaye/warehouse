import { useState } from 'react';
import { useToast } from '../components/Toast';
import { isWails } from '../api/client';
import { scan, check, exportReplenishment, type ReplenishmentItem } from '../api/replenishment';

const thS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', background: '#fafafa',
  textAlign: 'left', fontWeight: 600, fontSize: 13,
};
const tdS: React.CSSProperties = {
  border: '1px solid #ddd', padding: '8px 12px', fontSize: 13,
};
const btn: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #1890ff', background: '#1890ff', color: '#fff',
};
const btnGray: React.CSSProperties = {
  padding: '6px 12px', borderRadius: 4, cursor: 'pointer',
  fontSize: 13, border: '1px solid #d9d9d9', background: '#fff', color: '#333',
};
const inp: React.CSSProperties = {
  padding: '6px 10px', border: '1px solid #d9d9d9', borderRadius: 4,
  fontSize: 13, boxSizing: 'border-box' as const,
};
const sectionCard: React.CSSProperties = {
  border: '1px solid #e8e8e8', borderRadius: 6, padding: 16, marginBottom: 20,
};

export default function Replenishment() {
  const { showToast } = useToast();

  // ── scan section ──
  const [scanning, setScanning] = useState(false);
  const [scanResult, setScanResult] = useState<ReplenishmentItem[] | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);

  const handleScan = async () => {
    setScanning(true);
    setScanResult(null);
    setScanError(null);
    try {
      const res = await scan();
      setScanResult(res.items);
      if (res.items.length === 0) {
        showToast('success', '所有配件库存充足');
      } else {
        const shortage = res.items.filter(i => i.shortage > 0).length;
        showToast('success', `扫描完成，发现 ${shortage} 个告急配件`);
      }
    } catch (err: any) {
      const message = err?.error?.message || '扫描失败';
      setScanError(message);
      showToast('error', message);
    } finally {
      setScanning(false);
    }
  };

  // ── check section ──
  const [nameText, setNameText] = useState('');
  const [policy, setPolicy] = useState('default');
  const [customFactor, setCustomFactor] = useState('2');
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<{ items: ReplenishmentItem[]; notFound: string[] } | null>(null);

  const handleCheck = async () => {
    const names = nameText.split('\n').map(s => s.trim()).filter(Boolean);
    if (names.length === 0) { showToast('error', '请输入至少一个配件名称'); return; }
    setChecking(true);
    setCheckResult(null);
    try {
      const finalPolicy = policy === 'fixed' ? `fixed:${customFactor}` : 'default';
      const res = await check(names, finalPolicy);
      setCheckResult({ items: res.items, notFound: res.not_found });
      showToast('success', `检查完成，${res.items.length} 条结果`);
    } catch (err: any) {
      showToast('error', err?.error?.message || '检查失败');
    } finally {
      setChecking(false);
    }
  };

  // ── export scan result ──
  // Hits the backend xlsx endpoint, gets a Blob, then triggers a hidden
  // <a download> click so the browser saves the file. Same Wails guard as
  // AccessoryList: the embedded WebView doesn't honor <a download>.
  const [exporting, setExporting] = useState(false);
  const handleExport = async () => {
    if (exporting) return;
    if (isWails()) {
      showToast('warning', '请在浏览器中打开本应用后再导出文件');
      return;
    }
    setExporting(true);
    try {
      const blob = await exportReplenishment();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `告急补货_${formatStamp(new Date())}.xlsx`;
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

  // ── shortage row style ──
  const rowStyle = (item: ReplenishmentItem, idx: number): React.CSSProperties => ({
    background: item.shortage > 0
      ? '#fff1f0'
      : item.threshold === 0
        ? '#f5f5f5'
        : idx % 2 === 0 ? '#f9f9f9' : '#fff',
  });

  return (
    <div>
      <h2 style={{ margin: '0 0 16px' }}>告急补货</h2>

      {/* ── Section A: Full scan ── */}
      <div style={sectionCard}>
        <h3 style={{ margin: '0 0 8px' }}>全面扫描</h3>
        <p style={{ fontSize: 13, color: '#666', margin: '0 0 12px' }}>
          扫描所有配件库存，标记低于阈值的项目。
        </p>
        <button style={btn} disabled={scanning} onClick={handleScan}>
          {scanning ? '扫描中…' : '扫描告急'}
        </button>
        {scanResult && scanResult.length > 0 && (
          <button
            style={{ ...btnGray, marginLeft: 8 }}
            disabled={exporting}
            onClick={handleExport}
          >
            {exporting ? '导出中…' : '导出'}
          </button>
        )}

        {scanResult && scanResult.length > 0 && (
          <div style={{ marginTop: 12 }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={thS}>名称</th>
                  <th style={thS}>当前库存</th>
                  <th style={thS}>阈值</th>
                  <th style={thS}>缺货量</th>
                  <th style={thS}>建议补货</th>
                </tr>
              </thead>
              <tbody>
                {[...scanResult]
                  .sort((a, b) => b.shortage - a.shortage)
                  .map((item, idx) => (
                    <tr key={item.accessory_id} style={rowStyle(item, idx)}>
                      <td style={tdS}>{item.name}</td>
                      <td style={tdS}>{item.current_stock}</td>
                      <td style={tdS}>{item.threshold}</td>
                      <td style={{ ...tdS, color: item.shortage > 0 ? '#ff4d4f' : '#52c41a', fontWeight: 600 }}>
                        {item.shortage > 0 ? `缺 ${item.shortage}` : '充足'}
                      </td>
                      <td style={tdS}>{item.suggested_quantity}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        )}

        {scanResult && scanResult.length === 0 && (
          <div style={{ marginTop: 12, padding: '8px 12px', background: '#f6ffed', border: '1px solid #b7eb8f', borderRadius: 4, fontSize: 13 }}>
            所有配件库存充足。
          </div>
        )}

        {scanError && (
          <div style={{ marginTop: 12, padding: '8px 12px', background: '#fff2f0', border: '1px solid #ffccc7', borderRadius: 4, fontSize: 13 }}>
            扫描失败: {scanError}
          </div>
        )}
      </div>

      {/* ── Section B: Batch check ── */}
      <div style={sectionCard}>
        <h3 style={{ margin: '0 0 8px' }}>批量检查</h3>
        <p style={{ fontSize: 13, color: '#666', margin: '0 0 12px' }}>
          输入配件名称列表（每行一个），查询补货建议。
        </p>

        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 12 }}>
          <div style={{ flex: 1, minWidth: 280 }}>
            <label style={{ display: 'block', fontSize: 12, marginBottom: 4 }}>名称列表</label>
            <textarea
              style={{ ...inp, width: '100%', minHeight: 100, resize: 'vertical', fontFamily: 'monospace' }}
              placeholder={"充电器\n数据线\n保护壳"}
              value={nameText}
              onChange={e => setNameText(e.target.value)}
            />
          </div>
          <div style={{ minWidth: 160 }}>
            <label style={{ display: 'block', fontSize: 12, marginBottom: 4 }}>补货策略</label>
            <select style={{ ...inp, width: '100%' }} value={policy} onChange={e => setPolicy(e.target.value)}>
              <option value="default">默认</option>
              <option value="fixed">固定倍率</option>
            </select>
            {policy === 'fixed' && (
              <div style={{ marginTop: 4 }}>
                <label style={{ display: 'block', fontSize: 12, marginBottom: 2 }}>倍率</label>
                <input
                  style={inp}
                  type="number"
                  min={1}
                  value={customFactor}
                  onChange={e => setCustomFactor(e.target.value)}
                />
              </div>
            )}
          </div>
        </div>

        <button style={btn} disabled={checking} onClick={handleCheck}>
          {checking ? '检查中…' : '检查'}
        </button>

        {checkResult && (
          <div style={{ marginTop: 12 }}>
            {checkResult.notFound.length > 0 && (
              <div style={{ marginBottom: 8 }}>
                <span style={{ fontSize: 13, fontWeight: 600, color: '#ff4d4f' }}>未找到的名称: </span>
                <span style={{ fontSize: 13 }}>{checkResult.notFound.join(', ')}</span>
              </div>
            )}
            {checkResult.items.length > 0 && (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={thS}>名称</th>
                    <th style={thS}>当前库存</th>
                    <th style={thS}>阈值</th>
                    <th style={thS}>缺货量</th>
                    <th style={thS}>建议补货</th>
                  </tr>
                </thead>
                <tbody>
                  {checkResult.items
                    .sort((a, b) => b.shortage - a.shortage)
                    .map((item, idx) => (
                      <tr key={item.accessory_id} style={rowStyle(item, idx)}>
                        <td style={tdS}>{item.name}</td>
                        <td style={tdS}>{item.current_stock}</td>
                        <td style={tdS}>{item.threshold}</td>
                        <td style={{ ...tdS, color: item.shortage > 0 ? '#ff4d4f' : '#52c41a', fontWeight: 600 }}>
                          {item.shortage > 0 ? `缺 ${item.shortage}` : '充足'}
                        </td>
                        <td style={tdS}>{item.suggested_quantity}</td>
                      </tr>
                    ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
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