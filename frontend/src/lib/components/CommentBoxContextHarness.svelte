<script lang="ts">
  import { setContext } from "svelte";

  import {
    API_CLIENT_KEY,
    STORES_KEY,
  } from "../../../../packages/ui/src/context.js";
  import CommentBox from "../../../../packages/ui/src/components/detail/CommentBox.svelte";
  import IssueCommentBox from "../../../../packages/ui/src/components/detail/IssueCommentBox.svelte";

  interface AutocompleteResponse {
    users: string[];
    references: Array<{
      kind: string;
      number: number;
      title: string;
      state: string;
    }>;
  }

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
  export let autocompleteResponse: AutocompleteResponse = { users: [], references: [] };

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

  setContext(API_CLIENT_KEY, {
    GET: async (path: string) => {
      if (path === "/repos/{owner}/{name}/comment-autocomplete") {
        return { data: autocompleteResponse };
      }
      return { data: undefined, error: { title: "not mocked" } };
    },
  });
</script>

{#if kind === "pull"}
  <CommentBox {owner} {name} {number} />
{:else}
  <IssueCommentBox {owner} {name} {number} />
{/if}
