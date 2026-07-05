import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { ToastProvider } from '../components/Toast';
import Replenishment from './Replenishment';

vi.mock('../api/replenishment', () => ({
  scan: vi.fn(),
  check: vi.fn(),
}));

import { scan } from '../api/replenishment';

const mockScanResult = {
  items: [
    {
      accessory_id: 1,
      name: '告急配件',
      current_stock: 5,
      threshold: 20,
      shortage: 15,
      suggested_quantity: 15,
    },
    {
      accessory_id: 2,
      name: '充足配件',
      current_stock: 30,
      threshold: 10,
      shortage: -20,
      suggested_quantity: 0,
    },
  ],
};

describe('Replenishment', () => {
  beforeEach(() => {
    (scan as any).mockResolvedValue(mockScanResult);
  });

  it('renders the page title', () => {
    render(
      <ToastProvider>
        <Replenishment />
      </ToastProvider>
    );

    expect(screen.getByText('告急补货')).toBeInTheDocument();
  });

  it('scan button calls API and displays results', async () => {
    render(
      <ToastProvider>
        <Replenishment />
      </ToastProvider>
    );

    expect(screen.getByText('扫描告急')).toBeInTheDocument();

    fireEvent.click(screen.getByText('扫描告急'));

    await waitFor(() => {
      expect(screen.getByText('告急配件')).toBeInTheDocument();
    });

    // First item: shortage > 0
    expect(screen.getByText('缺 15')).toBeInTheDocument();
    expect(screen.getByText('15')).toBeInTheDocument(); // suggested_quantity

    // Second item: sufficient (should also be rendered)
    expect(screen.getByText('充足配件')).toBeInTheDocument();
  });
});