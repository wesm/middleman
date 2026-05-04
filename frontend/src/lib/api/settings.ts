import type { Settings } from "@middleman/ui/api/types";
import type { components } from "@middleman/ui/api/schema";

import { apiErrorMessage, client } from "./runtime.js";

type SettingsResponse = components["schemas"]["SettingsResponse"];
type RepoPreviewGeneratedResponse =
  components["schemas"]["RepoPreviewResponse"];
type UpdateSettingsRequest =
  components["schemas"]["UpdateSettingsRequest"];

function requestErrorMessage(
  error: { detail?: string; title?: string } | undefined,
  fallback: string,
): string {
  return apiErrorMessage(error, fallback);
}

function normalizeSettings(data: SettingsResponse): Settings {
  return {
    ...data,
    repos: data.repos ?? [],
  } as Settings;
}

export interface RepoPreviewRow {
  provider: string;
  platform_host: string;
  owner: string;
  name: string;
  repo_path: string;
  description: string | null;
  private: boolean;
  fork: boolean;
  pushed_at: string | null;
  already_configured: boolean;
}

export interface RepoPreviewResponse {
  provider: string;
  platform_host: string;
  owner: string;
  pattern: string;
  repos: RepoPreviewRow[];
}

export interface RepoRequestOptions {
  provider?: string;
  host?: string;
}

export interface RepoInput extends RepoRequestOptions {
  owner?: string;
  name?: string;
  repo_path?: string;
}

function repoQueryOptions(options: RepoRequestOptions): { provider?: string; platform_host?: string } {
  const query: { provider?: string; platform_host?: string } = {};
  if (options.provider) query.provider = options.provider;
  if (options.host) query.platform_host = options.host;
  return query;
}

function normalizePreviewResponse(
  data: RepoPreviewGeneratedResponse,
): RepoPreviewResponse {
  return {
    ...data,
    repos: data.repos ?? [],
  } as RepoPreviewResponse;
}

function normalizeUpdateRequest(
  settings: {
    activity?: Settings["activity"];
    terminal?: Settings["terminal"];
    agents?: Settings["agents"];
  },
): UpdateSettingsRequest {
  const request: UpdateSettingsRequest = {};
  if (settings.activity) {
    request.activity = settings.activity;
  }
  if (settings.terminal) {
    request.terminal = settings.terminal;
  }
  if (settings.agents) {
    request.agents = settings.agents.map((agent) => ({
      ...agent,
      command: agent.command ?? null,
    }));
  }
  return request;
}

export async function getSettings(): Promise<Settings> {
  const { data, error, response } = await client.GET("/settings");
  if (!data) {
    throw new Error(
      requestErrorMessage(
        error,
        `GET /settings -> ${response.status}`,
      ),
    );
  }
  return normalizeSettings(data);
}

export async function updateSettings(
  settings: {
    activity?: Settings["activity"];
    terminal?: Settings["terminal"];
    agents?: Settings["agents"];
  },
): Promise<Settings> {
  const { data, error, response } = await client.PUT("/settings", {
    body: normalizeUpdateRequest(settings),
  });
  if (!data) {
    throw new Error(
      requestErrorMessage(
        error,
        `PUT /settings -> ${response.status}`,
      ),
    );
  }
  return normalizeSettings(data);
}

export async function addRepo(
  owner: string,
  name: string,
  options: RepoRequestOptions = { provider: "github", host: "github.com" },
): Promise<Settings> {
  const { data, error, response } = await client.POST("/repos", {
    body: { ...options, owner, name },
  });
  if (!data) {
    throw new Error(
      requestErrorMessage(error, `POST /repos -> ${response.status}`),
    );
  }
  return normalizeSettings(data);
}

export async function removeRepo(
  owner: string,
  name: string,
  options: RepoRequestOptions = { provider: "github", host: "github.com" },
): Promise<void> {
  const { error, response } = await client.DELETE(
    "/repos/{owner}/{name}",
    {
      params: { path: { owner, name }, query: repoQueryOptions(options) },
    },
  );
  if (!response.ok) {
    throw new Error(
      requestErrorMessage(
        error,
        `DELETE /repos/{owner}/{name} -> ${response.status}`,
      ),
    );
  }
}

export async function refreshRepo(
  owner: string,
  name: string,
  options: RepoRequestOptions = { provider: "github", host: "github.com" },
): Promise<Settings> {
  const { data, error, response } = await client.POST(
    "/repos/{owner}/{name}/refresh",
    {
      params: { path: { owner, name }, query: repoQueryOptions(options) },
    },
  );
  if (!data) {
    throw new Error(
      requestErrorMessage(
        error,
        `POST /repos/{owner}/{name}/refresh -> ${response.status}`,
      ),
    );
  }
  return normalizeSettings(data);
}

export async function previewRepos(
  owner: string,
  pattern: string,
  options: RepoRequestOptions = { provider: "github", host: "github.com" },
): Promise<RepoPreviewResponse> {
  const { data, error, response } = await client.POST("/repos/preview", {
    body: { ...options, owner, pattern },
  });
  if (!data) {
    throw new Error(
      requestErrorMessage(error, `POST /repos/preview -> ${response.status}`),
    );
  }
  return normalizePreviewResponse(data);
}

export async function bulkAddRepos(repos: RepoInput[]): Promise<Settings> {
  const { data, error, response } = await client.POST("/repos/bulk", {
    body: {
      repos: repos.map((repo) => ({
        provider: "github",
        host: "github.com",
        ...repo,
      })),
    },
  });
  if (!data) {
    throw new Error(
      requestErrorMessage(error, `POST /repos/bulk -> ${response.status}`),
    );
  }
  return normalizeSettings(data);
}
