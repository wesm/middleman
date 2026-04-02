export interface EmbeddingHooks {
  actions?: {
    pullRequest?: ActionHook[];
    issue?: ActionHook[];
  };
}

export interface ActionHook {
  id: string;
  label: string;
  handler: (context: ActionContext) => void | Promise<void>;
}

export interface ActionContext {
  owner: string;
  name: string;
  number: number;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let _hooksValue: EmbeddingHooks | null = (window as any).__middleman_hooks ?? null;
let _generation = $state(0);
let _intercepted = false;

// Intercept assignments to window.__middleman_hooks so setting the
// property automatically triggers Svelte reactivity. Only install the
// interceptor when the property is absent or a plain data property.
const desc = Object.getOwnPropertyDescriptor(window, "__middleman_hooks");
if (!desc || (!desc.get && !desc.set && desc.configurable)) {
  Object.defineProperty(window, "__middleman_hooks", {
    get() { return _hooksValue; },
    set(val: EmbeddingHooks | null) {
      _hooksValue = val;
      _generation++;
    },
    configurable: true,
    enumerable: true,
  });
  _intercepted = true;
}

// Manual notify for hosts that mutate the hooks object in-place,
// or when the setter could not be installed.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(window as any).__middleman_notify_hooks_changed = () => {
  if (!_intercepted) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    _hooksValue = (window as any).__middleman_hooks ?? null;
  }
  _generation++;
};

function currentHooks(): EmbeddingHooks | null {
  void _generation; // reactive dependency
  if (!_intercepted) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    _hooksValue = (window as any).__middleman_hooks ?? null;
  }
  return _hooksValue;
}

export function getPullRequestActions(): ActionHook[] {
  return currentHooks()?.actions?.pullRequest ?? [];
}

export function getIssueActions(): ActionHook[] {
  return currentHooks()?.actions?.issue ?? [];
}

export function invokeAction(
  action: ActionHook,
  context: ActionContext,
): void {
  try {
    const result = action.handler(context);
    Promise.resolve(result).catch((err: unknown) => {
      console.error("Embedding action error:", err);
    });
  } catch (err) {
    console.error("Embedding action error:", err);
  }
}
