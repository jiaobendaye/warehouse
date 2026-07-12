import { useEffect } from 'react';
import { Routes, Route, NavLink, useNavigate } from 'react-router-dom';
import AccessoryList from './pages/AccessoryList';
import Inbound from './pages/Inbound';
import Calibration from './pages/Calibration';
import Outbound from './pages/Outbound';
import Flows from './pages/Flows';
import Replenishment from './pages/Replenishment';
import Settings from './pages/Settings';
import { isWails } from './api/client';
import { EventsOn } from '../wailsjs/runtime/runtime';

const navItems = [
  { to: '/accessories', label: '配件' },
  { to: '/inbound', label: '入库' },
  { to: '/calibration', label: '校准' },
  { to: '/outbound', label: '出库' },
  { to: '/flows', label: '流水' },
  { to: '/replenishment', label: '补货' },
];

export default function App() {
  const navigate = useNavigate();

  // In Wails mode the desktop menu emits 'navigate' events (see menu.go)
  // to push the user into pages like /settings that are not in the top nav.
  useEffect(() => {
    if (!isWails()) return;
    const off = EventsOn('navigate', (path: string) => {
      if (typeof path === 'string' && path.startsWith('/')) {
        navigate(path);
      }
    });
    return off;
  }, [navigate]);

  return (
    <div className="app">
      <nav style={{ display: 'flex', gap: 12, padding: '8px 16px', background: '#f5f5f5' }}>
        {navItems.map(({ to, label }) => (
          <NavLink
            key={to}
            to={to}
            style={({ isActive }) => ({
              fontWeight: isActive ? 'bold' : 'normal',
              textDecoration: 'none',
              color: isActive ? '#1890ff' : '#333',
            })}
          >
            {label}
          </NavLink>
        ))}
      </nav>
      <main style={{ padding: 16 }}>
        <Routes>
          <Route path="/" element={<AccessoryList />} />
          <Route path="/accessories" element={<AccessoryList />} />
          <Route path="/inbound" element={<Inbound />} />
          <Route path="/calibration" element={<Calibration />} />
          <Route path="/outbound" element={<Outbound />} />
          <Route path="/flows" element={<Flows />} />
          <Route path="/replenishment" element={<Replenishment />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </main>
    </div>
  );
}