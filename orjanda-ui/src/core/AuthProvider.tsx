// AuthProvider owns the token slots (PRD §15.4). Tokens live in localStorage
// so the WebSocket agent channel can re-attach them via ?access_token=
// (api/middleware/auth.go accepts the query fallback for browser WebSockets).

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { api } from '../api';
import type { LoginResponse } from '../types';

const ACCESS = 'orjanda.access_token';
const REFRESH = 'orjanda.refresh_token';

interface Identity {
  sub?: string;
  email?: string;
  name?: string;
  roles?: string[];
}

export interface AuthContextValue {
  token: string | null;
  identity: Identity | null;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<void>;
  refresh: () => Promise<boolean>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function decodeIdentity(token: string): Identity | null {
  try {
    const payload = token.split('.')[1];
    const parsed = JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')));
    return {
      sub: parsed.sub,
      email: parsed.email,
      name: parsed.name,
      roles: parsed.roles ?? parsed.role ? [parsed.role].flat() : undefined,
    };
  } catch {
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(ACCESS));
  const [identity, setIdentity] = useState<Identity | null>(() => {
    const t = localStorage.getItem(ACCESS);
    return t ? decodeIdentity(t) : null;
  });

  useEffect(() => {
    if (token) {
      localStorage.setItem(ACCESS, token);
    } else {
      localStorage.removeItem(ACCESS);
    }
  }, [token]);

  async function login(email: string, password: string): Promise<void> {
    const res = await api.post<LoginResponse>('/api/v1/auth/login', { email, password });
    localStorage.setItem(ACCESS, res.access_token);
    localStorage.setItem(REFRESH, res.refresh_token);
    setToken(res.access_token);
    setIdentity(decodeIdentity(res.access_token));
  }

  async function refresh(): Promise<boolean> {
    const refreshToken = localStorage.getItem(REFRESH);
    if (!refreshToken) return false;
    try {
      const res = await api.post<LoginResponse>('/api/v1/auth/refresh', {
        refresh_token: refreshToken,
      });
      localStorage.setItem(ACCESS, res.access_token);
      localStorage.setItem(REFRESH, res.refresh_token);
      setToken(res.access_token);
      setIdentity(decodeIdentity(res.access_token));
      return true;
    } catch {
      return false;
    }
  }

  function logout(): void {
    localStorage.removeItem(ACCESS);
    localStorage.removeItem(REFRESH);
    setToken(null);
    setIdentity(null);
  }

  return (
    <AuthContext.Provider
      value={{ token, identity, isAuthenticated: Boolean(token), login, refresh, logout }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
