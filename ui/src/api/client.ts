import type {
  GroupWithStats,
  Group,
  JobTemplate,
  Job,
  Runner,
  SystemStatus,
  ApiError,
  User,
  HistoryResponse,
  HistoryStatsResponse,
  HistoryStatsTimeRange,
} from '../types';
import { getConfig } from '../config';

class ApiClient {
  private token: string | null = null;

  private getApiBase(): string {
    return getConfig().apiUrl;
  }

  setToken(token: string | null) {
    this.token = token;
    if (token) {
      localStorage.setItem('auth_token', token);
    } else {
      localStorage.removeItem('auth_token');
    }
  }

  getToken(): string | null {
    if (!this.token) {
      this.token = localStorage.getItem('auth_token');
    }
    return this.token;
  }

  private async request<T>(
    path: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    const token = this.getToken();
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    const response = await fetch(`${this.getApiBase()}${path}`, {
      ...options,
      headers: {
        ...headers,
        ...options.headers,
      },
      credentials: 'include',
    });

    if (response.status === 401) {
      this.setToken(null);
      window.dispatchEvent(new CustomEvent('auth:logout'));
      throw new Error('Unauthorized');
    }

    if (!response.ok) {
      let errorMessage = `HTTP ${response.status}`;
      try {
        const error: ApiError = await response.json();
        errorMessage = error.error || errorMessage;
      } catch {
        // ignore parse errors
      }
      throw new Error(errorMessage);
    }

    // Handle 204 No Content
    if (response.status === 204) {
      return undefined as T;
    }

    return response.json();
  }

  // Auth
  async login(username: string, password: string): Promise<{ token: string; user: User }> {
    const result = await this.request<{ token: string; user: User }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    this.setToken(result.token);
    return result;
  }

  async logout(): Promise<void> {
    try {
      await this.request<void>('/auth/logout', { method: 'POST' });
    } finally {
      this.setToken(null);
    }
  }

  async getCurrentUser(): Promise<User> {
    return this.request<User>('/auth/me');
  }

  getGitHubAuthUrl(): string {
    return `${this.getApiBase()}/auth/github`;
  }

  // Groups
  async getGroups(): Promise<GroupWithStats[]> {
    return this.request<GroupWithStats[]>('/groups');
  }

  async getGroup(id: string): Promise<Group> {
    return this.request<Group>(`/groups/${id}`);
  }

  // Job Templates
  async getJobTemplates(groupId: string): Promise<JobTemplate[]> {
    return this.request<JobTemplate[]>(`/groups/${groupId}/templates`);
  }

  async getJobTemplate(id: string): Promise<JobTemplate> {
    return this.request<JobTemplate>(`/templates/${id}`);
  }

  // Queue / Jobs
  async getQueue(groupId: string): Promise<Job[]> {
    return this.request<Job[]>(`/groups/${groupId}/queue`);
  }

  async getHistory(
    groupId: string,
    limit = 50,
    before?: string,
    filters?: {
      statuses?: ('completed' | 'failed' | 'cancelled')[];
      labels?: Record<string, string>;
    }
  ): Promise<HistoryResponse> {
    const params = new URLSearchParams();
    params.set('limit', limit.toString());

    if (before) {
      params.set('before', before);
    }

    if (filters?.statuses && filters.statuses.length > 0) {
      params.set('status', filters.statuses.join(','));
    }

    if (filters?.labels) {
      for (const [key, value] of Object.entries(filters.labels)) {
        params.set(`label.${key}`, value);
      }
    }

    return this.request<HistoryResponse>(`/groups/${groupId}/history?${params.toString()}`);
  }

  async getHistoryStats(
    groupId: string,
    range: HistoryStatsTimeRange = 'auto'
  ): Promise<HistoryStatsResponse> {
    const params = new URLSearchParams();
    params.set('range', range);
    return this.request<HistoryStatsResponse>(`/groups/${groupId}/history/stats?${params.toString()}`);
  }

  async getJob(id: string): Promise<Job> {
    return this.request<Job>(`/jobs/${id}`);
  }

  async createJob(
    groupId: string,
    templateId: string,
    inputs?: Record<string, string>,
    autoRequeue?: boolean,
    requeueLimit?: number | null
  ): Promise<Job> {
    return this.request<Job>(`/groups/${groupId}/queue`, {
      method: 'POST',
      body: JSON.stringify({
        template_id: templateId,
        inputs,
        auto_requeue: autoRequeue,
        requeue_limit: requeueLimit,
      }),
    });
  }

  async updateJob(id: string, inputs: Record<string, string>): Promise<Job> {
    return this.request<Job>(`/jobs/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ inputs }),
    });
  }

  async deleteJob(id: string): Promise<void> {
    await this.request<void>(`/jobs/${id}`, { method: 'DELETE' });
  }

  async pauseJob(id: string): Promise<Job> {
    return this.request<Job>(`/jobs/${id}/pause`, { method: 'POST' });
  }

  async unpauseJob(id: string): Promise<Job> {
    return this.request<Job>(`/jobs/${id}/unpause`, { method: 'POST' });
  }

  async cancelJob(id: string): Promise<Job> {
    return this.request<Job>(`/jobs/${id}/cancel`, { method: 'POST' });
  }

  async disableAutoRequeue(id: string): Promise<Job> {
    return this.request<Job>(`/jobs/${id}/disable-requeue`, { method: 'POST' });
  }

  async updateAutoRequeue(id: string, autoRequeue: boolean, requeueLimit?: number | null): Promise<Job> {
    return this.request<Job>(`/jobs/${id}/auto-requeue`, {
      method: 'PUT',
      body: JSON.stringify({ auto_requeue: autoRequeue, requeue_limit: requeueLimit }),
    });
  }

  async reorderQueue(groupId: string, jobIds: string[]): Promise<void> {
    await this.request<void>(`/groups/${groupId}/queue/reorder`, {
      method: 'PUT',
      body: JSON.stringify({ job_ids: jobIds }),
    });
  }

  // Runners
  async getRunners(groupId: string): Promise<Runner[]> {
    return this.request<Runner[]>(`/groups/${groupId}/runners`);
  }

  async getAllRunners(): Promise<Runner[]> {
    return this.request<Runner[]>('/runners');
  }

  async refreshRunners(): Promise<void> {
    await this.request<void>('/runners/refresh', { method: 'POST' });
  }

  // System
  async getStatus(): Promise<SystemStatus> {
    return this.request<SystemStatus>('/status');
  }

  async getHealth(): Promise<{ status: string }> {
    const response = await fetch('/health');
    return response.json();
  }

  // WebSocket URL
  getWebSocketUrl(): string {
    const token = this.getToken();
    const apiBase = this.getApiBase();

    // If apiBase is a full URL, parse it to get the WebSocket URL
    if (apiBase.startsWith('http://') || apiBase.startsWith('https://')) {
      const url = new URL(apiBase);
      const wsProtocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
      return `${wsProtocol}//${url.host}${url.pathname}/ws?token=${token || ''}`;
    }

    // Relative URL - use current host
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${window.location.host}${apiBase}/ws?token=${token || ''}`;
  }
}

export const api = new ApiClient();
