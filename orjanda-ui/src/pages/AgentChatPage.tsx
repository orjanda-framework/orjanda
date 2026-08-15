// AgentChatPage: the embedded Agent Chat UI (PRD §23, §38.2, TAD §6.2).
// Renders streaming tokens, tool activity, and the full approval round trip:
// Approve / Deny / Modify with the policy reason surfaced for every write.

import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useAgent } from '../core/useAgent';
import type { AgentServerEvent } from '../types';

function ApprovalCard({
  event,
  onRespond,
}: {
  event: Extract<AgentServerEvent, { type: 'approval_required' }>;
  onRespond: (actionId: string, approved: boolean, payload?: Record<string, unknown>) => void;
}) {
  const [modifying, setModifying] = useState(false);
  const [payloadText, setPayloadText] = useState(() => JSON.stringify(event.details.payload ?? {}, null, 2));

  return (
    <div className="rounded-md border border-amber-300 bg-amber-50 p-4">
      <div className="mb-1 flex items-center justify-between">
        <span className="text-sm font-semibold text-amber-900">Approval required</span>
        <span className="rounded bg-amber-100 px-2 py-0.5 text-xs text-amber-800">
          {event.details.action} · {event.details.doctype}
        </span>
      </div>
      <p className="mb-3 text-sm text-amber-900">{event.details.policy_reason}</p>

      {modifying && (
        <textarea
          value={payloadText}
          onChange={(e) => setPayloadText(e.target.value)}
          rows={5}
          className="mb-3 w-full rounded-md border border-amber-300 bg-white px-3 py-2 font-mono text-xs"
        />
      )}

      <div className="flex flex-wrap gap-2">
        <button
          onClick={() => onRespond(event.action_id, true)}
          className="rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-emerald-700"
        >
          Approve
        </button>
        <button
          onClick={() => onRespond(event.action_id, false)}
          className="rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-700"
        >
          Deny
        </button>
        <button
          onClick={() => {
            if (modifying) {
              let payload: Record<string, unknown> | undefined;
              try {
                payload = JSON.parse(payloadText);
              } catch {
                // Keep current payload if the text is invalid JSON.
              }
              onRespond(event.action_id, true, payload);
            } else {
              setModifying(true);
            }
          }}
          className="rounded-md border border-amber-500 px-3 py-1.5 text-sm font-medium text-amber-900 hover:bg-amber-100"
        >
          {modifying ? 'Submit modified' : 'Modify'}
        </button>
        {modifying && (
          <button
            onClick={() => setModifying(false)}
            className="rounded-md px-3 py-1.5 text-sm text-slate-500 hover:bg-amber-100"
          >
            Cancel
          </button>
        )}
      </div>
    </div>
  );
}

export function AgentChatPage() {
  const { state, events, connect, disconnect, sendMessage, respond, reset } = useAgent();
  const [input, setInput] = useState('');
  const bottomRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    connect();
    return () => disconnect();
  }, [connect, disconnect]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [events]);

  const approvals = events.filter(
    (e): e is Extract<AgentServerEvent, { type: 'approval_required' }> => e.type === 'approval_required',
  );

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const text = input.trim();
    if (!text) return;
    setInput('');
    sendMessage(text);
  }

  // Group messages by sender for proper rendering
  const tokenEvents = events.filter((e): e is Extract<AgentServerEvent, { type: 'token' }> => e.type === 'token' && Boolean(e.content));

  // Group consecutive tokens from the same sender into single messages
  const messages: Array<{ content: string; sender: 'user' | 'assistant' }> = [];
  let currentMessage: { content: string; sender: 'user' | 'assistant' } | null = null;

  for (const event of tokenEvents) {
    const sender = event.sender || 'assistant';
    const content = event.content || '';
    if (currentMessage && currentMessage.sender === sender) {
      currentMessage.content += content;
    } else {
      if (currentMessage) {
        messages.push(currentMessage);
      }
      currentMessage = { content, sender };
    }
  }
  if (currentMessage) {
    messages.push(currentMessage);
  }

  return (
    <div className="flex h-[calc(100vh-8rem)] flex-col rounded-lg border border-slate-200 bg-white shadow-sm">
      <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
        <h2 className="text-lg font-semibold text-slate-900">Agent Chat</h2>
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${
              state === 'open' ? 'bg-emerald-500' : state === 'connecting' ? 'bg-amber-400' : 'bg-slate-300'
            }`}
          />
          <span className="text-xs text-slate-500">{state}</span>
          <button onClick={reset} className="text-xs text-slate-400 hover:text-slate-600">
            Clear
          </button>
        </div>
      </div>

      <div className="flex-1 space-y-3 overflow-y-auto p-4">
        {approvals.map((e) => (
          <ApprovalCard key={e.action_id} event={e} onRespond={respond} />
        ))}
        {messages.map((e, i) => (
          <div
            key={i}
            className={`flex ${e.sender === 'user' ? 'justify-end' : 'justify-start'}`}
          >
            <div
              className={`max-w-[80%] whitespace-pre-wrap rounded-md px-3 py-2 text-sm ${
                e.sender === 'user'
                  ? 'bg-indigo-600 text-white'
                  : 'bg-slate-50 text-slate-800'
              }`}
            >
              {e.content}
            </div>
          </div>
        ))}
        {events
          .filter((e): e is AgentServerEvent & { type: 'tool_start' } => e.type === 'tool_start')
          .map((e, i) => (
            <div key={i} className="flex items-center gap-2 text-xs text-slate-500">
              <span>▶</span>
              <span className="font-mono">{e.tool}</span>
            </div>
          ))}
        {events
          .filter((e): e is AgentServerEvent & { type: 'tool_end' } => e.type === 'tool_end')
          .map((e, i) => (
            <div key={i} className="flex items-center gap-2 text-xs text-slate-500">
              <span className={e.success === false ? 'text-red-500' : ''}>
                {e.success === false ? '✕' : '✓'}
              </span>
              <span className="font-mono">{e.tool} done</span>
            </div>
          ))}
        {events.length === 0 && (
          <p className="text-center text-sm text-slate-400">
            Ask the agent to do something, e.g. “create a leave request for next week”.
          </p>
        )}
        <div ref={bottomRef} />
      </div>

      <form onSubmit={onSubmit} className="flex gap-2 border-t border-slate-200 p-3">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Message the agent…"
          className="flex-1 rounded-md border border-slate-300 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
        />
        <button
          type="submit"
          disabled={state !== 'open'}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
        >
          Send
        </button>
      </form>
    </div>
  );
}
