import { Routes, Route, NavLink } from 'react-router-dom';
import AccessoryList from './pages/AccessoryList';
import Inbound from './pages/Inbound';
import Outbound from './pages/Outbound';
import Flows from './pages/Flows';
import Replenishment from './pages/Replenishment';
import Settings from './pages/Settings';

const navItems = [
  { to: '/accessories', label: '配件' },
  { to: '/inbound', label: '入库' },
  { to: '/outbound', label: '出库' },
  { to: '/flows', label: '流水' },
  { to: '/replenishment', label: '补货' },
  { to: '/settings', label: '⚙' },
];

export default function App() {
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
          <Route path="/outbound" element={<Outbound />} />
          <Route path="/flows" element={<Flows />} />
          <Route path="/replenishment" element={<Replenishment />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </main>
    </div>
  );
}