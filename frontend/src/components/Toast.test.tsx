import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { ToastProvider, useToast } from './Toast';

function TestComponent({ type = 'success' as 'success' | 'error', message = 'Test message' }) {
  const { showToast } = useToast();
  return <button onClick={() => showToast(type, message)}>Show Toast</button>;
}

describe('Toast', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders toast when showToast is called', () => {
    render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Show Toast'));

    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Test message');
    expect(alert).toHaveStyle({ backgroundColor: 'rgb(82, 196, 26)' }); // success green
  });

  it('renders error toast with red background', () => {
    render(
      <ToastProvider>
        <TestComponent type="error" message="Error occurred" />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Show Toast'));

    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Error occurred');
    expect(alert).toHaveStyle({ backgroundColor: 'rgb(255, 77, 79)' }); // error red
  });

  it('auto-dismisses after 3 seconds', () => {
    vi.useFakeTimers();
    render(
      <ToastProvider>
        <TestComponent />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Show Toast'));
    expect(screen.getByRole('alert')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(3000);
    });

    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
