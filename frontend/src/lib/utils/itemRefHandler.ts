import { client } from "../api/runtime.js";
import { navigate } from "../stores/router.svelte.js";
import { showFlash } from "../stores/flash.svelte.js";

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
): Promise<void> {
  const { data, error } = await client.GET(
    "/repos/{owner}/{name}/items/{number}",
    { params: { path: { owner, name, number } } },
  );

  if (error) {
    showFlash(`Item ${owner}/${name}#${number} not found on GitHub.`);
    return;
  }

  if (!data.repo_tracked) {
    showFlash(
      `${owner}/${name} is not tracked. Add it in Settings to navigate here.`,
    );
    return;
  }

  const path = data.item_type === "pr"
    ? `/pulls/${owner}/${name}/${number}`
    : `/issues/${owner}/${name}/${number}`;
  navigate(path);
}

function handleClick(e: MouseEvent): void {
  // Let browser handle modified clicks (cmd, ctrl, shift, middle).
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;

  const anchor = findItemRef(e.target);
  if (!anchor) return;

  const owner = anchor.dataset.owner;
  const name = anchor.dataset.name;
  const numberStr = anchor.dataset.number;
  if (!owner || !name || !numberStr) return;

  e.preventDefault();
  void resolveAndNavigate(owner, name, parseInt(numberStr, 10));
}

export function initItemRefHandler(): () => void {
  document.addEventListener("click", handleClick);
  return () => document.removeEventListener("click", handleClick);
}
