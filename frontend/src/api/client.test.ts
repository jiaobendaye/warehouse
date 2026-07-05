import { describe, it, expect, vi, beforeEach } from 'vitest';
import { isWails, getTransport } from './client';

describe('transport adapter', () => {
  beforeEach(() => {
    delete (window as any).runtime;
    delete (window as any).go;
  });

  describe('isWails', () => {
    it('returns false in browser (jsdom)', () => {
      expect(isWails()).toBe(false);
    });

    it('returns true when window.runtime is set', () => {
      (window as any).runtime = {};
      expect(isWails()).toBe(true);
    });

    it('returns true when window.go is set (Wails v2)', () => {
      (window as any).go = {};
      expect(isWails()).toBe(true);
    });
  });

  describe('getTransport', () => {
    it('returns "http" by default', () => {
      expect(getTransport()).toBe('http');
    });

    it('returns "wails" when runtime available', () => {
      (window as any).runtime = {};
      expect(getTransport()).toBe('wails');
    });
  });
});