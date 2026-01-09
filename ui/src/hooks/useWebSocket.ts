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
  // Flag to prevent reconnect and close pending connections when intentionally disconnecting
  const isDisconnectingRef = useRef(false);

  // Store options and queryClient in refs to avoid stale closures
  const optionsRef = useRef(options);
  const queryClientRef = useRef(queryClient);
  // Ref to hold the connect function for self-referencing in reconnect
  const connectRef = useRef<() => void>(() => {});

  // Update refs when values change
  useEffect(() => {
    optionsRef.current = options;
    queryClientRef.current = queryClient;
  }, [options, queryClient]);

  const connect = useCallback(() => {
    // Reset the disconnecting flag when connecting
    isDisconnectingRef.current = false;

    const token = api.getToken();
    if (!token) {
      return;
    }

    const wsUrl = api.getWebSocketUrl();

    const handleMessage = (message: WSMessage) => {
      const { type, group_id: groupId, payload } = message;
      const opts = optionsRef.current;
      const qc = queryClientRef.current;

      switch (type) {
        case 'runner_status':
          if (payload && groupId) {
            opts.onRunnerStatus?.(payload as Runner, groupId);
            qc.invalidateQueries({ queryKey: ['runners', groupId] });
            qc.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'queue_update':
          if (payload && groupId) {
            opts.onQueueUpdate?.(payload as Job[], groupId);
            qc.invalidateQueries({ queryKey: ['queue', groupId] });
            qc.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'job_state':
          if (payload) {
            const job = payload as Job;
            opts.onJobState?.(job);
            qc.invalidateQueries({ queryKey: ['job', job.id] });
            qc.invalidateQueries({ queryKey: ['queue', job.group_id] });
            qc.invalidateQueries({ queryKey: ['history', job.group_id] });
            qc.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'dispatch':
          if (payload) {
            const job = payload as Job;
            opts.onDispatch?.(job);
            qc.invalidateQueries({ queryKey: ['queue', job.group_id] });
            qc.invalidateQueries({ queryKey: ['groups'] });
          }
          break;

        case 'error':
          if (payload) {
            opts.onError?.(String(payload));
          }
          break;
      }
    };

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        // If disconnect was called while we were connecting, close immediately
        if (isDisconnectingRef.current) {
          ws.close();
          return;
        }
        setIsConnected(true);
        // Re-subscribe to all groups
        subscribedGroupsRef.current.forEach((groupId) => {
          ws.send(JSON.stringify({ type: 'subscribe', group_id: groupId }));
        });
      };

      ws.onclose = () => {
        setIsConnected(false);
        wsRef.current = null;
        // Only attempt to reconnect if we're not intentionally disconnecting
        if (!isDisconnectingRef.current) {
          reconnectTimeoutRef.current = setTimeout(() => connectRef.current(), 3000);
        }
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
      // Connection failed, will retry using ref
      if (!isDisconnectingRef.current) {
        reconnectTimeoutRef.current = setTimeout(() => connectRef.current(), 3000);
      }
    }
  }, []);

  // Keep connectRef updated with current connect function
  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

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
    isDisconnectingRef.current = true;
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = undefined;
    }
    if (wsRef.current) {
      // Only close if already open - if still connecting, the onopen handler
      // will check isDisconnectingRef and close it then
      if (wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.close();
      }
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
