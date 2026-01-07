// API Types - matching the Go backend

export type JobStatus = 'pending' | 'triggered' | 'running' | 'completed' | 'failed' | 'cancelled';

export type RunnerStatus = 'online' | 'offline';

export type Role = 'readonly' | 'admin';

export type AuthProvider = 'basic' | 'github';

export interface Group {
  id: string;
  name: string;
  description: string;
  runner_labels: string[];
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface GroupWithStats extends Group {
  queued_jobs: number;
  running_jobs: number;
  idle_runners: number;
  busy_runners: number;
  total_runners: number;
  template_count: number;
}

export interface JobTemplate {
  id: string;
  group_id: string;
  name: string;
  owner: string;
  repo: string;
  workflow_id: string;
  ref: string;
  default_inputs: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface Job {
  id: string;
  group_id: string;
  template_id: string;
  priority: number;
  position: number;
  status: JobStatus;
  paused: boolean;
  auto_requeue: boolean;
  requeue_limit: number | null;
  requeue_count: number;
  inputs: Record<string, string>;
  created_by: string;
  triggered_at: string | null;
  run_id: number | null;
  run_url: string;
  runner_name: string;
  completed_at: string | null;
  error_message: string;
  created_at: string;
  updated_at: string;
}

export interface HistoryResponse {
  jobs: Job[];
  has_more: boolean;
  next_cursor?: string;
}

export interface Runner {
  id: number;
  name: string;
  labels: string[];
  status: RunnerStatus;
  busy: boolean;
  os: string;
  last_seen_at: string;
  created_at: string;
  updated_at: string;
}

export interface User {
  id: string;
  username: string;
  role: Role;
  auth_provider: AuthProvider;
  github_id?: string;
  created_at: string;
  updated_at: string;
}

export interface ApiError {
  error: string;
}

export interface SystemStatus {
  status: string;
  version: string;
  timestamp: string;
}

// WebSocket message types
export type WSMessageType =
  | 'runner_status'
  | 'queue_update'
  | 'job_state'
  | 'dispatch'
  | 'system_status'
  | 'subscribe'
  | 'unsubscribe'
  | 'ping'
  | 'pong';

export interface WSMessage<T = unknown> {
  type: WSMessageType;
  payload: T;
}

export interface WSRunnerStatus {
  group_id: string;
  runner: Runner;
}

export interface WSQueueUpdate {
  group_id: string;
  jobs: Job[];
}

export interface WSJobState {
  job_id: string;
  group_id: string;
  previous_status: JobStatus;
  new_status: JobStatus;
  run_id?: number;
  runner_name?: string;
  error_message?: string;
}

export interface WSDispatch {
  job_id: string;
  group_id: string;
  run_id: number;
  workflow: string;
}

export interface WSSystemStatus {
  dispatcher_running: boolean;
  github_rate_limit_remaining: number;
  connected_clients: number;
  timestamp: string;
}
