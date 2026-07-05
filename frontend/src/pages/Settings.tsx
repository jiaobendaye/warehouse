import { useState, useCallback, useEffect } from 'react';
import { isWails } from '../api/client';
import * as App from '../../wailsjs/go/desktop/App';

export default function Settings() {
  const [serverRunning, setServerRunning] = useState(false);
  const [serverAddr, setServerAddr] = useState('');
  const [statusLoading, setStatusLoading] = useState(false);

  const refreshStatus = useCallback(async () => {
    if (!isWails()) {
      // Browser mode: server is always running (we are accessing via HTTP)
      setServerRunning(true);
      setServerAddr(window.location.host);
      return;
    }
    setStatusLoading(true);
    try {
      const running = await App.IsServerRunning();
      setServerRunning(running);
      if (running) {
        const addr = await App.ServerAddr();
        setServerAddr(addr);
      }
    } catch {
      // Bindings not available yet
    } finally {
      setStatusLoading(false);
    }
  }, []);

  // Refresh on mount.
  useEffect(() => {
    refreshStatus();
  }, [refreshStatus]);

  const handleToggle = async () => {
    if (!isWails()) return;
    setStatusLoading(true);
    try {
      if (serverRunning) {
        await App.StopServer();
        setServerRunning(false);
      } else {
        await App.StartServer();
        setServerRunning(true);
        setServerAddr(await App.ServerAddr());
      }
    } catch (e: any) {
      console.error('Server toggle error:', e);
    } finally {
      setStatusLoading(false);
    }
  };

  return (
    <div>
      <h2 style={{ margin: '0 0 16px' }}>设置</h2>

      <div style={{
        border: '1px solid #e8e8e8', borderRadius: 8, padding: 20,
        maxWidth: 500, background: '#fafafa',
      }}>
        <h3 style={{ margin: '0 0 12px' }}>Web 服务</h3>

        <div style={{ marginBottom: 12, fontSize: 14 }}>
          <div style={{ marginBottom: 8 }}>
            状态：
            <span style={{
              fontWeight: 'bold',
              color: serverRunning ? '#52c41a' : '#ff4d4f',
              marginLeft: 8,
            }}>
              {serverRunning ? '● 运行中' : '○ 已停止'}
            </span>
          </div>
          {serverRunning && serverAddr && (
            <div style={{ marginBottom: 8 }}>
              地址：<code style={{ background: '#f5f5f5', padding: '2px 6px', borderRadius: 3 }}>
                http://{serverAddr}
              </code>
            </div>
          )}
          <div style={{ color: '#888', fontSize: 12, marginBottom: 12 }}>
            {serverRunning
              ? 'REST API + MCP + 前端页面均可用。浏览器可访问上方地址。'
              : 'Web 服务未启动，仅 GUI 模式可用。'}
          </div>
        </div>

        {isWails() && (
          <button
            onClick={handleToggle}
            disabled={statusLoading}
            style={{
              padding: '8px 20px', borderRadius: 4, cursor: 'pointer',
              fontSize: 14, border: 'none',
              background: serverRunning ? '#ff4d4f' : '#1890ff',
              color: '#fff',
            }}
          >
            {statusLoading ? '处理中…' : serverRunning ? '停止 Web 服务' : '启动 Web 服务'}
          </button>
        )}

        {!isWails() && (
          <p style={{ color: '#888', fontSize: 13 }}>
            浏览器模式下 Web 服务始终运行。
          </p>
        )}
      </div>

      {/* MCP section */}
      <div style={{
        border: '1px solid #e8e8e8', borderRadius: 8, padding: 20,
        maxWidth: 500, marginTop: 16, background: '#fafafa',
      }}>
        <h3 style={{ margin: '0 0 12px' }}>MCP 服务</h3>
        <p style={{ fontSize: 13, color: '#666', margin: 0 }}>
          MCP 端点随 Web 服务一同启停，地址为{' '}
          <code style={{ background: '#f5f5f5', padding: '2px 6px', borderRadius: 3 }}>
            http://{serverAddr || '127.0.0.1:17880'}/mcp
          </code>
        </p>
        <p style={{ fontSize: 12, color: '#999', marginTop: 8 }}>
          外部 AI 代理可通过此端点调用仓库管理能力。
        </p>
      </div>
    </div>
  );
}