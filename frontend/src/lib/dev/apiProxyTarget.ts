import { readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { load as loadToml } from "js-toml";

const defaultHost = "127.0.0.1";
const defaultPort = 8091;

export const defaultDevApiUrl = `http://${defaultHost}:${defaultPort}`;

export interface DevEnv {
  HOME?: string;
  MIDDLEMAN_API_URL?: string;
  MIDDLEMAN_CONFIG?: string;
  MIDDLEMAN_HOME?: string;
}

interface MiddlemanConfigFields {
  basePath?: string;
  host?: string;
  port?: number;
}

export function resolveDevApiUrl(env: DevEnv = process.env): string {
  const override = env.MIDDLEMAN_API_URL?.trim();
  if (override) {
    return override;
  }

  try {
    const configText = readFileSync(resolveConfigPath(env), "utf8");
    return buildDevApiUrl(parseConfig(configText));
  } catch {
    return defaultDevApiUrl;
  }
}

function resolveConfigPath(env: DevEnv): string {
  const explicitConfigPath = env.MIDDLEMAN_CONFIG?.trim();
  if (explicitConfigPath) {
    return explicitConfigPath;
  }

  const middlemanHome = env.MIDDLEMAN_HOME?.trim();
  if (middlemanHome) {
    return path.join(middlemanHome, "config.toml");
  }

  const home = env.HOME?.trim() || os.homedir();
  return path.join(home, ".config", "middleman", "config.toml");
}

function buildDevApiUrl(config: MiddlemanConfigFields): string {
  const host = normalizeHost(config.host);
  const port = normalizePort(config.port);
  const basePath = normalizeBasePath(config.basePath);
  const url = new URL(`http://${formatHostForUrl(host)}:${port}`);

  if (basePath) {
    url.pathname = `${basePath}/`;
    return `${url.origin}${basePath}`;
  }

  return url.origin;
}

function normalizeHost(host: string | undefined): string {
  const value = host?.trim();
  return value || defaultHost;
}

function normalizePort(port: number | undefined): number {
  if (typeof port !== "number" || !Number.isInteger(port)) {
    return defaultPort;
  }
  if (port < 1 || port > 65535) {
    return defaultPort;
  }
  return port;
}

function normalizeBasePath(basePath: string | undefined): string {
  const value = basePath?.trim();
  if (!value || value === "/") {
    return "";
  }

  return `/${value.replace(/^\/+|\/+$/g, "")}`;
}

function formatHostForUrl(host: string): string {
  if (host.includes(":") && !host.startsWith("[") && !host.endsWith("]")) {
    return `[${host}]`;
  }
  return host;
}

function parseConfig(configText: string): MiddlemanConfigFields {
  const config = loadToml(configText) as Record<string, unknown>;
  const parsed: MiddlemanConfigFields = {};

  const host = parseStringField(config.host);
  if (host !== undefined) {
    parsed.host = host;
  }

  const basePath = parseStringField(config.base_path);
  if (basePath !== undefined) {
    parsed.basePath = basePath;
  }

  const port = parseIntegerField(config.port);
  if (port !== undefined) {
    parsed.port = port;
  }

  return parsed;
}

function parseStringField(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function parseIntegerField(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isInteger(value)) {
    return value;
  }

  if (
    typeof value === "bigint" &&
    value >= BigInt(Number.MIN_SAFE_INTEGER) &&
    value <= BigInt(Number.MAX_SAFE_INTEGER)
  ) {
    return Number(value);
  }

  return undefined;
}
