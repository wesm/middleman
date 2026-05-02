import { client } from "../api/runtime.js";
import { navigate, buildItemRoute } from "../stores/router.svelte.js";
import { showFlash } from "../stores/flash.svelte.js";

let requestId = 0;

function findItemRef(target: EventTarget | null): HTMLAnchorElement | null {
  let el = target as HTMLElement | null;
  while (el) {
    if (el instanceof HTMLAnchorElement && el.classList.contains("item-ref")) {
      return el;
    }
    el = el.parentElement;
  }
  return null;
}

async function resolveAndNavigate(
  owner: string,
  name: string,
  number: number,
  platformHost: string | undefined,
  thisRequestId: number,
): Promise<void> {
  try {
    const { data, error, response } = await client.POST(
      "/repos/{owner}/{name}/items/{number}/resolve",
      {
        params: {
          path: { owner, name, number },
          ...(platformHost ? { query: { platform_host: platformHost } } : {}),
        },
      },
    );

    // A newer click superseded this one — discard the result.
    if (thisRequestId !== requestId) return;

    if (error) {
      if (response.status === 404) {
        showFlash(`Item ${owner}/${name}#${number} not found on GitHub.`);
      } else {
        showFlash(`Failed to resolve ${owner}/${name}#${number}. Try again later.`);
      }
      return;
    }

    if (!data.repo_tracked) {
      showFlash(
        `${owner}/${name} is not tracked. Add it in Settings to navigate here.`,
      );
      return;
    }

    const path = buildItemRoute(
      data.item_type === "pr" ? "pr" : "issue",
      owner,
      name,
      number,
      platformHost,
    );
    navigate(path);
  } catch {
    if (thisRequestId !== requestId) return;
    showFlash("Failed to resolve item reference. Check your connection.");
  }
}

function handleClick(e: MouseEvent): void {
  // Let browser handle modified clicks (cmd, ctrl, shift, middle).
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;

  const anchor = findItemRef(e.target);
  if (!anchor) return;

  const owner = anchor.dataset.owner;
  const name = anchor.dataset.name;
  const numberStr = anchor.dataset.number;
  const platformHost = anchor.dataset.platformHost;
  if (!owner || !name || !numberStr) return;

  e.preventDefault();
  requestId++;
  void resolveAndNavigate(
    owner,
    name,
    parseInt(numberStr, 10),
    platformHost,
    requestId,
  );
}

export function initItemRefHandler(): () => void {
  document.addEventListener("click", handleClick);
  return () => document.removeEventListener("click", handleClick);
}
