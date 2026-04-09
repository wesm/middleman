<script lang="ts">
  import { setContext } from "svelte";

  import { STORES_KEY } from "../../../../packages/ui/src/context.js";
  import CommentBox from "../../../../packages/ui/src/components/detail/CommentBox.svelte";
  import IssueCommentBox from "../../../../packages/ui/src/components/detail/IssueCommentBox.svelte";

  export let kind: "pull" | "issue";
  export let owner = "octo";
  export let name = "repo";
  export let number = 1;
  export let submitComment: (
    owner: string,
    name: string,
    number: number,
    body: string,
  ) => Promise<void> = async () => {};
  export let getError: () => string | null = () => null;

  setContext(STORES_KEY, {
    detail: {
      submitComment,
      getDetailError: getError,
    },
    issues: {
      submitIssueComment: submitComment,
      getIssueDetailError: getError,
    },
  });
</script>

{#if kind === "pull"}
  <CommentBox {owner} {name} {number} />
{:else}
  <IssueCommentBox {owner} {name} {number} />
{/if}
