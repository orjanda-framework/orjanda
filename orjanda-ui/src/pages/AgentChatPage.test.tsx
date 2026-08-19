// Agent Chat approval round-trip acceptance test (PRD §38.2): a server
// approval_required event renders the policy reason and the Approve / Deny /
// Modify controls, and each control sends the corresponding approval_response
// over the §6.2 WebSocket channel.

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { AgentServerEvent } from '../types';
import { AgentChatPage } from './AgentChatPage';

class FakeWebSocket {
  static OPEN = 1;
  static instances: FakeWebSocket[] = [];
  readyState = FakeWebSocket.OPEN;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(_url: string) {
    FakeWebSocket.instances.push(this);
    setTimeout(() => this.onopen?.(), 0);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = FakeWebSocket.OPEN;
  }

  static emit(evt: AgentServerEvent): void {
    for (const ws of FakeWebSocket.instances) {
      ws.onmessage?.({ data: JSON.stringify(evt) });
    }
  }

  static lastSent(): string[] {
    const all = FakeWebSocket.instances.flatMap((ws) => ws.sent);
    return all;
  }
}

const approvalEvent: AgentServerEvent = {
  type: 'approval_required',
  action_id: 'act-123',
  details: {
    doctype: 'LeaveRequest',
    action: 'create',
    payload: { start_date: '2026-09-01', reason: 'vacation' },
    policy_reason: 'Bulk write of type create exceeds the safe threshold (AlwaysRequireApproval policy).',
  },
};

beforeEach(() => {
  FakeWebSocket.instances = [];
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
});

afterEach(() => {
  delete (globalThis as Record<string, unknown>).WebSocket;
});

describe('AgentChatPage approval round trip', () => {
  it('sends a message with correct type and field names', async () => {
    render(<AgentChatPage />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));

    const input = screen.getByPlaceholderText('Message the agent...');
    const sendButton = screen.getByRole('button', { name: 'Send' });
    
    fireEvent.change(input, { target: { value: 'test message' } });
    fireEvent.click(sendButton);

    await waitFor(() => {
      const sent = FakeWebSocket.lastSent();
      expect(sent.length).toBeGreaterThan(0);
    });
    const msg = JSON.parse(FakeWebSocket.lastSent()[0]);
    expect(msg.type).toBe('message');
    expect(msg.text).toBe('test message');
  });

  it('renders Approve/Deny/Modify and sends an approval_response on Approve', async () => {
    render(<AgentChatPage />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));

    act(() => FakeWebSocket.emit(approvalEvent));

    expect(await screen.findByText(/Approval required/)).toBeInTheDocument();
    expect(screen.getByText(/Bulk write of type create exceeds/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Deny' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Modify' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));

    await waitFor(() => {
      const sent = FakeWebSocket.lastSent();
      expect(sent.length).toBeGreaterThan(0);
    });
    const msg = JSON.parse(FakeWebSocket.lastSent()[0]);
    expect(msg.type).toBe('approval_response');
    expect(msg.action_id).toBe('act-123');
    expect(msg.approved).toBe(true);
  });

  it('sends an approval_response with approved:false on Deny', async () => {
    render(<AgentChatPage />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    act(() => FakeWebSocket.emit(approvalEvent));
    expect(await screen.findByText(/Approval required/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Deny' }));

    await waitFor(() => expect(FakeWebSocket.lastSent().length).toBeGreaterThan(0));
    const msg = JSON.parse(FakeWebSocket.lastSent()[0]);
    expect(msg.type).toBe('approval_response');
    expect(msg.action_id).toBe('act-123');
    expect(msg.approved).toBe(false);
  });

  it('sends a modified payload via the Modify flow', async () => {
    const { container } = render(<AgentChatPage />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    act(() => FakeWebSocket.emit(approvalEvent));
    expect(await screen.findByText(/Approval required/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Modify' }));
    const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    fireEvent.change(textarea, {
      target: { value: JSON.stringify({ start_date: '2026-10-01', reason: 'family leave' }) },
    });
    fireEvent.click(screen.getByRole('button', { name: /Submit modified/ }));

    await waitFor(() => expect(FakeWebSocket.lastSent().length).toBeGreaterThan(0));
    const msg = JSON.parse(FakeWebSocket.lastSent()[0]);
    expect(msg.type).toBe('approval_response');
    expect(msg.approved).toBe(true);
    expect(msg.payload).toEqual({ start_date: '2026-10-01', reason: 'family leave' });
  });
});
