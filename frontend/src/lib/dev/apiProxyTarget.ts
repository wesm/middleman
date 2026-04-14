import { readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const defaultHost = "127.0.0.1";
const defaultPort = 8091;

export const defaultDevApiUrl = `http://${defaultHost}:${defaultPort}`;

export interface DevEnv {
  HOME?: string;
  MIDDLEMAN_API_URL?: string;
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
  const config: MiddlemanConfigFields = {};

  for (const rawLine of configText.split(/\r?\n/u)) {
    const line = stripComments(rawLine).trim();
    if (!line) {
      continue;
    }

    const match = line.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+)$/u);
    if (!match) {
      continue;
    }

    const key = match[1];
    const rawValue = match[2];
    if (!key || !rawValue) {
      continue;
    }

    switch (key) {
      case "host": {
        const host = parseTomlString(rawValue);
        if (host !== undefined) {
          config.host = host;
        }
        break;
      }
      case "base_path": {
        const basePath = parseTomlString(rawValue);
        if (basePath !== undefined) {
          config.basePath = basePath;
        }
        break;
      }
      case "port": {
        const port = parseTomlInteger(rawValue);
        if (port !== undefined) {
          config.port = port;
        }
        break;
      }
    }
  }

  return config;
}

function stripComments(line: string): string {
  let inString = false;
  let escaped = false;

  for (let i = 0; i < line.length; i += 1) {
    const char = line[i];

    if (char === '"' && !escaped) {
      inString = !inString;
    } else if (char === "#" && !inString) {
      return line.slice(0, i);
    }

    escaped = char === "\\" && !escaped;
    if (char !== "\\") {
      escaped = false;
    }
  }

  return line;
}

function parseTomlString(value: string): string | undefined {
  const match = value.trim().match(/^"((?:[^"\\]|\\.)*)"$/u);
  if (!match) {
    return undefined;
  }

  return JSON.parse(`"${match[1]}"`) as string;
}

function parseTomlInteger(value: string): number | undefined {
  const match = value.trim().match(/^[+-]?\d+$/u);
  if (!match) {
    return undefined;
  }

  const parsed = Number.parseInt(match[0], 10);
  return Number.isNaN(parsed) ? undefined : parsed;
}
