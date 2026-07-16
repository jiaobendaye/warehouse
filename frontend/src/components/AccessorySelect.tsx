import { useState, useRef, useEffect, useMemo } from 'react';
import type { Accessory } from '../api/accessory';

interface Props {
  accessories: Accessory[];
  value: number | '';
  onChange: (v: number | '') => void;
  placeholder?: string;
  width?: number | string;
}

const inpStyle: React.CSSProperties = {
  padding: '6px 10px', border: '1px solid #d9d9d9', borderRadius: 4,
  fontSize: 13, boxSizing: 'border-box' as const, width: '100%',
};

// AccessorySelect — a searchable combobox for picking an accessory by name.
// Replaces the plain <select> so users can type to filter when the catalog
// is large. Value/onChange keep the `number | ''` contract every page already
// uses, so swapping is a drop-in.
export default function AccessorySelect({ accessories, value, onChange, placeholder = '-- 请选择 --', width }: Props) {
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [hi, setHi] = useState(0); // highlighted index
  const wrapRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const selected = useMemo(
    () => accessories.find(a => a.id === value) || null,
    [accessories, value]
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return accessories;
    return accessories.filter(a => a.name.toLowerCase().includes(q));
  }, [accessories, query]);

  // Display: search text while open+typing, otherwise the selected name.
  const display = open ? query : (selected ? selected.name : '');

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
        setQuery('');
      }
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  const openList = () => {
    if (!open) {
      setOpen(true);
      setQuery('');
      setHi(0);
    }
  };

  const pick = (id: number | '') => {
    onChange(id);
    setOpen(false);
    setQuery('');
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        openList();
        e.preventDefault();
      }
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHi(i => Math.min(i + 1, filtered.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHi(i => Math.max(i - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (filtered[hi]) pick(filtered[hi].id);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      setOpen(false);
      setQuery('');
    }
  };

  const wrapStyle: React.CSSProperties = { position: 'relative', width: width ?? '100%' };

  return (
    <div ref={wrapRef} style={wrapStyle}>
      <input
        ref={inputRef}
        style={inpStyle}
        value={display}
        placeholder={value === '' ? placeholder : ''}
        onChange={e => { openList(); setQuery(e.target.value); setHi(0); }}
        onFocus={openList}
        onKeyDown={onKey}
      />
      {open && (
        <div style={{
          position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 1000,
          background: '#fff', border: '1px solid #d9d9d9', borderRadius: 4,
          maxHeight: 240, overflowY: 'auto', marginTop: 2,
          boxShadow: '0 2px 8px rgba(0,0,0,0.12)',
        }}>
          {filtered.length === 0 && (
            <div style={{ padding: '8px 10px', fontSize: 13, color: '#999' }}>无匹配项</div>
          )}
          {filtered.map((a, i) => (
            <div
              key={a.id}
              onMouseDown={e => { e.preventDefault(); pick(a.id); }}
              onMouseEnter={() => setHi(i)}
              style={{
                padding: '6px 10px', fontSize: 13, cursor: 'pointer',
                background: i === hi ? '#e6f7ff' : '#fff',
                color: a.id === value ? '#1890ff' : '#333',
                fontWeight: a.id === value ? 600 : 400,
              }}
            >
              {a.name}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
