<script lang="ts">
  import type { ActivityItem } from "../api/types.js";
  import ActivityFeed
    from "../components/ActivityFeed.svelte";
  import DetailDrawer
    from "../components/DetailDrawer.svelte";

  type DrawerItem = {
    itemType: "pr" | "issue";
    platformHost?: string | undefined;
    owner: string;
    name: string;
    number: number;
  };

  interface Props {
    drawerItem?: DrawerItem | null;
    onSelectItem?: (item: ActivityItem) => void;
    onCloseDrawer?: () => void;
  }

  let {
    drawerItem: controlledDrawer,
    onSelectItem,
    onCloseDrawer,
  }: Props = $props();

  // Internal state used when no controlled props are
  // provided (standalone usage).
  let internalDrawer = $state<DrawerItem | null>(null);

  const controlled = $derived(
    controlledDrawer !== undefined || onCloseDrawer !== undefined,
  );
  const activeDrawer = $derived(
    controlled ? (controlledDrawer ?? null) : internalDrawer,
  );

  function handleSelect(item: ActivityItem): void {
    const itemType =
      item.item_type === "issue" ? "issue" : "pr";
    const entry: DrawerItem = {
      itemType,
      platformHost: item.platform_host,
      owner: item.repo_owner,
      name: item.repo_name,
      number: item.item_number,
    };
    if (!controlled) {
      internalDrawer = entry;
    }
    onSelectItem?.(item);
  }

  function handleClose(): void {
    if (!controlled) {
      internalDrawer = null;
    }
    onCloseDrawer?.();
  }
</script>

<div class="activity-layout">
  <ActivityFeed onSelectItem={handleSelect} />
  {#if activeDrawer}
    <DetailDrawer
      itemType={activeDrawer.itemType}
      platformHost={activeDrawer.platformHost}
      owner={activeDrawer.owner}
      name={activeDrawer.name}
      number={activeDrawer.number}
      onClose={handleClose}
    />
  {/if}
</div>

<style>
  .activity-layout {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    position: relative;
  }
</style>
