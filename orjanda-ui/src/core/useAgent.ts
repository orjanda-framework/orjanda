// useAgent: WebSocket client for the Agent Chat channel (TAD §6.2). The
// channel is opened with the access token as a query parameter because browser
// WebSockets cannot set Authorization headers. Incoming events are the flat
// §6.2 shapes; approval round-trips send approval_response messages (§12.3).

import { useCallback, useRef, useState } from 'react';
import type { AgentServerEvent, ApprovalResponseMessage } from '../types';

type ConnState = 'idle' | 'connecting' | 'open' | 'closed';

export interface AgentChatApi {
  state: ConnState;
  events: AgentServerEvent[];
  connect: () => void;
  disconnect: () => void;
  sendMessage: (text: string) => void;
  respond: (actionId: string, approved: boolean, payload?: Record<string, unknown>) => void;
  reset: () => void;
}

function wsUrl(): string {
  const base = window.location.host;
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  const token = localStorage.getItem('orjanda.access_token') ?? '';
  return `${proto}://${base}/api/v1/agent/stream?access_token=${encodeURIComponent(token)}`;
}

export function useAgent(): AgentChatApi {
  const [state, setState] = useState<ConnState>('idle');
  const [events, setEvents] = useState<AgentServerEvent[]>([]);
  const socketRef = useRef<WebSocket | null>(null);

  const connect = useCallback(() => {
    if (socketRef.current?.readyState === WebSocket.OPEN) return;
    setState('connecting');
    const ws = new WebSocket(wsUrl());
    socketRef.current = ws;

    ws.onopen = () => setState('open');
    ws.onclose = () => {
      setState('closed');
      socketRef.current = null;
    };
    ws.onerror = () => {
      setState('closed');
      socketRef.current = null;
    };
    ws.onmessage = (msg) => {
      try {
        const evt = JSON.parse(msg.data as string) as AgentServerEvent;
        setEvents((prev) => [...prev, evt]);
      } catch {
        // Ignore malformed frames.
      }
    };
  }, []);

  const disconnect = useCallback(() => {
    socketRef.current?.close();
    socketRef.current = null;
    setState('closed');
  }, []);

  const send = useCallback((body: unknown) => {
    const ws = socketRef.current;
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(body));
    }
  }, []);

  const sendMessage = useCallback(
    (text: string) => {
      setEvents((prev) => [...prev, { type: 'token', content: text }]);
      send({ type: 'user_message', content: text });
    },
    [send],
  );

  const respond = useCallback(
    (actionId: string, approved: boolean, payload?: Record<string, unknown>) => {
      const msg: ApprovalResponseMessage = { type: 'approval_response', action_id: actionId, approved };
      if (payload) msg.payload = payload;
      send(msg);
    },
    [send],
  );

  const reset = useCallback(() => setEvents([]), []);

  return { state, events, connect, disconnect, sendMessage, respond, reset };
}
