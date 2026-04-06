import { getContext } from "svelte";
import type {
  MiddlemanClient,
  ActionRegistry,
  NavigateCallback,
  EventCallback,
  PrepareRouteCallback,
  StoreInstances,
  UIConfig,
  SidebarAccessors,
} from "./types.js";

export const API_CLIENT_KEY = Symbol("middleman-api-client");
export const ACTIONS_KEY = Symbol("middleman-actions");
export const NAVIGATE_KEY = Symbol("middleman-navigate");
export const EVENT_KEY = Symbol("middleman-event");
export const PREPARE_ROUTE_KEY = Symbol("middleman-prepare-route");
export const STORES_KEY = Symbol("middleman-stores");
export const UI_CONFIG_KEY = Symbol("middleman-ui-config");
export const SIDEBAR_KEY = Symbol("middleman-sidebar");

export function getClient(): MiddlemanClient {
  return getContext(API_CLIENT_KEY);
}
export function getActions(): ActionRegistry {
  return getContext(ACTIONS_KEY);
}
export function getNavigate(): NavigateCallback {
  return getContext(NAVIGATE_KEY);
}
export function getEventCallback(): EventCallback {
  return getContext(EVENT_KEY);
}
export function getPrepareRoute(): PrepareRouteCallback | null {
  return getContext(PREPARE_ROUTE_KEY);
}
export function getStores(): StoreInstances {
  return getContext(STORES_KEY);
}
export function getUIConfig(): UIConfig {
  return getContext(UI_CONFIG_KEY);
}
export function getSidebar(): SidebarAccessors {
  return getContext(SIDEBAR_KEY);
}
