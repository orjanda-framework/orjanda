import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useAgent } from '@/core/useAgent';
import type { AgentServerEvent } from '@/types';
import {
  MessageScrollerProvider,
  MessageScroller,
  MessageScrollerViewport,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerButton,
} from '@/components/ui/message-scroller';
import { Message, MessageContent } from '@/components/ui/message';
import { Bubble, BubbleContent } from '@/components/ui/bubble';
import { Marker, MarkerContent } from '@/components/ui/marker';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Separator } from '@/components/ui/separator';
import { SendIcon, PlayIcon, CheckIcon, XIcon } from 'lucide-react';

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
    <Card className="border-warning">
      <CardContent>
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold text-foreground">Approval required</span>
            <Badge variant="secondary">
              {event.details.action} . {event.details.doctype}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">{event.details.policy_reason}</p>

          {modifying && (
            <Textarea
              value={payloadText}
              onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setPayloadText(e.target.value)}
              rows={5}
              className="font-mono text-xs"
            />
          )}

          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              onClick={() => onRespond(event.action_id, true)}
            >
              <CheckIcon data-icon="inline-start" />
              Approve
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => onRespond(event.action_id, false)}
            >
              <XIcon data-icon="inline-start" />
              Deny
            </Button>
            <Button
              size="sm"
              variant="outline"
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
            >
              {modifying ? 'Submit modified' : 'Modify'}
            </Button>
            {modifying && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setModifying(false)}
              >
                Cancel
              </Button>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

export function AgentChatPage() {
  const { state, events, connect, disconnect, sendMessage, respond, reset } = useAgent();
  const [input, setInput] = useState('');

  useEffect(() => {
    connect();
    return () => disconnect();
  }, [connect, disconnect]);

  const approvals = useMemo(
    () => events.filter(
      (e): e is Extract<AgentServerEvent, { type: 'approval_required' }> => e.type === 'approval_required',
    ),
    [events],
  );

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const text = input.trim();
    if (!text) return;
    setInput('');
    sendMessage(text);
  }

  const tokenEvents = events.filter(
    (e): e is Extract<AgentServerEvent, { type: 'token' }> => e.type === 'token' && Boolean(e.content),
  );

  const messages: Array<{ content: string; sender: 'user' | 'assistant'; id: number }> = [];
  let currentMessage: { content: string; sender: 'user' | 'assistant'; id: number } | null = null;

  for (let i = 0; i < tokenEvents.length; i++) {
    const event = tokenEvents[i];
    const sender = event.sender || 'assistant';
    const content = event.content || '';
    if (currentMessage && currentMessage.sender === sender) {
      currentMessage.content += content;
    } else {
      if (currentMessage) {
        messages.push(currentMessage);
      }
      currentMessage = { content, sender, id: i };
    }
  }
  if (currentMessage) {
    messages.push(currentMessage);
  }

  const toolStarts = events.filter(
    (e): e is AgentServerEvent & { type: 'tool_start' } => e.type === 'tool_start',
  );
  const toolEnds = events.filter(
    (e): e is AgentServerEvent & { type: 'tool_end' } => e.type === 'tool_end',
  );

  return (
    <div className="flex h-[calc(100vh-8rem)] flex-col rounded-lg border bg-card">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <h2 className="text-lg font-semibold text-foreground">Agent Chat</h2>
        <div className="flex items-center gap-2">
          <span
            className={`size-2 rounded-full ${
              state === 'open' ? 'bg-green-500' : state === 'connecting' ? 'bg-yellow-400' : 'bg-muted-foreground/40'
            }`}
          />
          <span className="text-xs text-muted-foreground">{state}</span>
          <Separator orientation="vertical" className="h-4" />
          <Button variant="ghost" size="sm" onClick={reset}>
            Clear
          </Button>
        </div>
      </div>

      <MessageScrollerProvider autoScroll>
        <MessageScroller className="flex-1">
          <MessageScrollerViewport>
            <MessageScrollerContent className="gap-4 p-4">
              {approvals.map((e) => (
                <MessageScrollerItem key={e.action_id} messageId={e.action_id}>
                  <ApprovalCard event={e} onRespond={respond} />
                </MessageScrollerItem>
              ))}

              {messages.map((msg) => (
                <MessageScrollerItem
                  key={msg.id}
                  messageId={String(msg.id)}
                  scrollAnchor={msg.sender === 'user'}
                >
                  <Message align={msg.sender === 'user' ? 'end' : 'start'}>
                    <MessageContent>
                      <Bubble
                        variant={msg.sender === 'user' ? 'default' : 'secondary'}
                        align={msg.sender === 'user' ? 'end' : 'start'}
                      >
                        <BubbleContent>
                          <span className="whitespace-pre-wrap">{msg.content}</span>
                        </BubbleContent>
                      </Bubble>
                    </MessageContent>
                  </Message>
                </MessageScrollerItem>
              ))}

              {toolStarts.map((e, i) => (
                <MessageScrollerItem key={`ts-${i}`} messageId={`tool-start-${i}`}>
                  <Marker>
                    <PlayIcon className="text-muted-foreground" />
                    <MarkerContent>
                      <code className="text-xs">{e.tool}</code>
                    </MarkerContent>
                  </Marker>
                </MessageScrollerItem>
              ))}

              {toolEnds.map((e, i) => (
                <MessageScrollerItem key={`te-${i}`} messageId={`tool-end-${i}`}>
                  <Marker>
                    {e.success === false ? (
                      <XIcon className="text-destructive" />
                    ) : (
                      <CheckIcon className="text-green-600" />
                    )}
                    <MarkerContent>
                      <code className="text-xs">{e.tool} done</code>
                    </MarkerContent>
                  </Marker>
                </MessageScrollerItem>
              ))}

              {events.length === 0 && (
                <MessageScrollerItem messageId="empty" scrollAnchor>
                  <div className="flex items-center justify-center py-12">
                    <p className="text-center text-sm text-muted-foreground">
                      Ask the agent to do something, e.g. "create a leave request for next week".
                    </p>
                  </div>
                </MessageScrollerItem>
              )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>

      <form onSubmit={onSubmit} className="flex gap-2 border-t p-3">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Message the agent..."
          className="flex-1"
        />
        <Button type="submit" disabled={state !== 'open'}>
          <SendIcon data-icon="inline-start" />
          Send
        </Button>
      </form>
    </div>
  );
}
