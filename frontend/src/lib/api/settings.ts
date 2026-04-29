import type { Settings } from "@middleman/ui/api/types";
import type { components } from "@middleman/ui/api/schema";

import { apiErrorMessage, client } from "./runtime.js";

type SettingsResponse = components["schemas"]["SettingsResponse"];
type RepoPreviewGeneratedResponse =
  components["schemas"]["RepoPreviewResponse"];

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
  owner: string;
  name: string;
  description: string | null;
  private: boolean;
  fork: boolean;
  pushed_at: string | null;
  already_configured: boolean;
}

export interface RepoPreviewResponse {
  owner: string;
  pattern: string;
  repos: RepoPreviewRow[];
}

interface RepoInput {
  owner: string;
  name: string;
}

function normalizePreviewResponse(
  data: RepoPreviewGeneratedResponse,
): RepoPreviewResponse {
  return {
    ...data,
    repos: data.repos ?? [],
  } as RepoPreviewResponse;
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
  },
): Promise<Settings> {
  const { data, error, response } = await client.PUT("/settings", {
    body: settings,
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
): Promise<Settings> {
  const { data, error, response } = await client.POST("/repos", {
    body: { owner, name },
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
): Promise<void> {
  const { error, response } = await client.DELETE(
    "/repos/{owner}/{name}",
    {
      params: { path: { owner, name } },
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
): Promise<Settings> {
  const { data, error, response } = await client.POST(
    "/repos/{owner}/{name}/refresh",
    {
      params: { path: { owner, name } },
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
): Promise<RepoPreviewResponse> {
  const { data, error, response } = await client.POST("/repos/preview", {
    body: { owner, pattern },
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
    body: { repos },
  });
  if (!data) {
    throw new Error(
      requestErrorMessage(error, `POST /repos/bulk -> ${response.status}`),
    );
  }
  return normalizeSettings(data);
}
