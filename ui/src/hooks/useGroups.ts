import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';

export function useGroups() {
  return useQuery({
    queryKey: ['groups'],
    queryFn: () => api.getGroups(),
    staleTime: 30_000,
  });
}

export function useGroup(id: string) {
  return useQuery({
    queryKey: ['groups', id],
    queryFn: () => api.getGroup(id),
    enabled: !!id,
    staleTime: 30_000,
  });
}

export function useJobTemplates(groupId: string) {
  return useQuery({
    queryKey: ['templates', groupId],
    queryFn: () => api.getJobTemplates(groupId),
    enabled: !!groupId,
    staleTime: 60_000,
  });
}

export function useQueue(groupId: string) {
  return useQuery({
    queryKey: ['queue', groupId],
    queryFn: () => api.getQueue(groupId),
    enabled: !!groupId,
    staleTime: 10_000,
    refetchInterval: 30_000,
  });
}

export function useRunners(groupId: string) {
  return useQuery({
    queryKey: ['runners', groupId],
    queryFn: () => api.getRunners(groupId),
    enabled: !!groupId,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}

export function useAllRunners() {
  return useQuery({
    queryKey: ['runners'],
    queryFn: () => api.getAllRunners(),
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}

export function useSystemStatus() {
  return useQuery({
    queryKey: ['status'],
    queryFn: () => api.getStatus(),
    staleTime: 10_000,
    refetchInterval: 30_000,
  });
}
