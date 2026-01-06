import { useEffect, useRef, useCallback, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import type { Job, Runner } from '../types';

type MessageType =
  | 'runner_status'
  | 'queue_update'
  | 'job_state'
  | 'dispatch'
  | 'system_status'
  | 'subscribed'
  | 'unsubscribed'
  | 'error';

interface WSMessage {
  type: MessageType;
  group_id?: string;
  payload?: unknown;
}

interface UseWebSocketOptions {
  onRunnerStatus?: (runner: Runner, groupId: string) => void;
  onQueueUpdate?: (jobs: Job[], groupId: string) => void;
  onJobState?: (job: Job) => void;
  onDispatch?: (job: Job) => void;
  onError?: (error: string) => void;
}

export function useWebSocket(options: UseWebSocketOptions = {}) {
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const subscribedGroupsRef = useRef<Set<string>>(new Set());
  const queryClient = useQueryClient();
  const [isConnected, setIsConnected] = useState(false);

  const connect = useCallback(() => {
    const token = api.getToken();
    if (!token) {
      return;
    }

    const wsUrl = api.getWebSocketUrl();

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setIsConnected(true);
        // Re-subscribe to all groups
        subscribedGroupsRef.current.forEach((groupId) => {
          ws.send(JSON.stringify({ type: 'subscribe', group_id: groupId }));
        });
      };

      ws.onclose = () => {
        setIsConnected(false);
        wsRef.current = null;
        // Attempt to reconnect after 3 seconds
        reconnectTimeoutRef.current = setTimeout(connect, 3000);
      };

      ws.onerror = () => {
        setIsConnected(false);
      };

      ws.onmessage = (event) => {
        try {
          const message: WSMessage = JSON.parse(event.data);
          handleMessage(message);
        } catch {
          console.error('Failed to parse WebSocket message');
        }
      };
    } catch {
      // Connection failed, will retry
      reconnectTimeoutRef.current = setTimeout(connect, 3000);
    }
  }, []);

  const handleMessage = useCallback(
    (message: WSMessage) => {
      const { type, group_id: groupId, payload } = message;

      switch (type) {
        case 'runner_status':
          if (payload && groupId) {
            options.onRunnerStatus?.(payload as Runner, groupId);
            // Invalidate runners query
            queryClient.invalidateQueries({ queryKey: ['runners', groupId] });
            queryClient.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'queue_update':
          if (payload && groupId) {
            options.onQueueUpdate?.(payload as Job[], groupId);
            // Invalidate queue query
            queryClient.invalidateQueries({ queryKey: ['queue', groupId] });
            queryClient.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'job_state':
          if (payload) {
            const job = payload as Job;
            options.onJobState?.(job);
            // Invalidate job and queue queries
            queryClient.invalidateQueries({ queryKey: ['job', job.id] });
            queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
            queryClient.invalidateQueries({ queryKey: ['history', job.group_id] });
            queryClient.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'dispatch':
          if (payload) {
            const job = payload as Job;
            options.onDispatch?.(job);
            queryClient.invalidateQueries({ queryKey: ['queue', job.group_id] });
            queryClient.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'error':
          if (payload) {
            options.onError?.(String(payload));
          }
          break;
      }
    },
    [queryClient, options]
  );

  const subscribe = useCallback((groupId: string) => {
    subscribedGroupsRef.current.add(groupId);
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'subscribe', group_id: groupId }));
    }
  }, []);

  const unsubscribe = useCallback((groupId: string) => {
    subscribedGroupsRef.current.delete(groupId);
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'unsubscribe', group_id: groupId }));
    }
  }, []);

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
    }
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setIsConnected(false);
  }, []);

  useEffect(() => {
    connect();
    return () => {
      disconnect();
    };
  }, [connect, disconnect]);

  return {
    isConnected,
    subscribe,
    unsubscribe,
    disconnect,
    reconnect: connect,
  };
}
