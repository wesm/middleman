<script>
  import { setContext } from "svelte";

  import { STORES_KEY } from "../../../../packages/ui/src/context.js";
  import CommentBox from "../../../../packages/ui/src/components/detail/CommentBox.svelte";
  import IssueCommentBox from "../../../../packages/ui/src/components/detail/IssueCommentBox.svelte";

  export let visible;
  export let kind;
  export let owner = "octo";
  export let name = "repo";
  export let number = 1;

  setContext(STORES_KEY, {
    detail: {
      submitComment: async () => {},
      getDetailError: () => null,
    },
    issues: {
      submitIssueComment: async () => {},
      getIssueDetailError: () => null,
    },
  });
</script>

{#if visible}
  {#if kind === "pull"}
    <CommentBox {owner} {name} {number} />
  {:else}
    <IssueCommentBox {owner} {name} {number} />
  {/if}
{/if}
