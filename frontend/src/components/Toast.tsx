import { createContext, useContext, useState, useCallback, useRef, useEffect, type ReactNode } from 'react';

type ToastType = 'success' | 'error';

interface ToastState {
  type: ToastType;
  message: string;
}

interface ToastContextValue {
  toast: ToastState | null;
  showToast: (type: ToastType, message: string) => void;
}

const ToastContext = createContext<ToastContextValue>({
  toast: null,
  showToast: () => {},
});

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastState | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showToast = useCallback((type: ToastType, message: string) => {
    if (timerRef.current) clearTimeout(timerRef.current);
    setToast({ type, message });
    timerRef.current = setTimeout(() => {
      setToast(null);
      timerRef.current = null;
    }, 3000);
  }, []);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  return (
    <ToastContext.Provider value={{ toast, showToast }}>
      {children}
      {toast && (
        <div
          role="alert"
          style={{
            position: 'fixed',
            top: 20,
            right: 20,
            padding: '12px 24px',
            borderRadius: 4,
            color: '#fff',
            backgroundColor: toast.type === 'success' ? '#52c41a' : '#ff4d4f',
            zIndex: 9999,
            boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
            fontSize: 14,
            transition: 'opacity 0.3s',
          }}
        >
          {toast.message}
        </div>
      )}
    </ToastContext.Provider>
  );
}

export function useToast() {
  return useContext(ToastContext);
}
