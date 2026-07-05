import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../components/Toast';
import AccessoryList from './AccessoryList';

vi.mock('../api/accessory', () => ({
  listAccessories: vi.fn(),
  createAccessory: vi.fn(),
  updateAccessory: vi.fn(),
  deleteAccessory: vi.fn(),
}));

import { listAccessories } from '../api/accessory';

const mockItems = [
  {
    id: 1,
    name: '测试螺丝',
    current_stock: 100,
    low_stock_threshold: 20,
    notes: '',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 2,
    name: '测试螺母',
    current_stock: 50,
    low_stock_threshold: 10,
    notes: '易耗品',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
];

describe('AccessoryList', () => {
  beforeEach(() => {
    (listAccessories as any).mockResolvedValue({
      items: mockItems,
      total: 2,
      limit: 10,
      offset: 0,
    });
  });

  it('renders the page title', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <AccessoryList />
        </ToastProvider>
      </MemoryRouter>
    );

    expect(screen.getByText('配件列表')).toBeInTheDocument();
  });

  it('renders table with mock data', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <AccessoryList />
        </ToastProvider>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText('测试螺丝')).toBeInTheDocument();
    });

    expect(screen.getByText('100')).toBeInTheDocument();
    expect(screen.getByText('测试螺母')).toBeInTheDocument();
  });

  it('shows search input and create button', async () => {
    render(
      <MemoryRouter>
        <ToastProvider>
          <AccessoryList />
        </ToastProvider>
      </MemoryRouter>
    );

    expect(screen.getByPlaceholderText(/搜索/)).toBeInTheDocument();
    expect(screen.getByText('新建配件')).toBeInTheDocument();
  });
});