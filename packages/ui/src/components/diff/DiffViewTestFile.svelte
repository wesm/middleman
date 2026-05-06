<script lang="ts">
  import type { DiffFile } from "../../api/types.js";

  interface Props {
    file: DiffFile;
  }

  const { file }: Props = $props();
</script>

<div data-file-path={file.path}>{file.path}</div>
