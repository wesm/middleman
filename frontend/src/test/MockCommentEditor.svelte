<script lang="ts">
  interface Props {
    owner: string;
    name: string;
    value: string;
    disabled?: boolean;
    placeholder?: string;
    oninput: (value: string) => void;
    onsubmit: () => void;
  }

  let {
    owner,
    name,
    value,
    disabled = false,
    placeholder = "Write a comment...",
    oninput,
    onsubmit,
  }: Props = $props();
</script>

<textarea
  data-testid="mock-comment-editor"
  data-owner={owner}
  data-name={name}
  aria-label={placeholder}
  {placeholder}
  {disabled}
  value={value}
  oninput={(event) => oninput(event.currentTarget.value)}
  onkeydown={(event) => {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      onsubmit();
    }
  }}
></textarea>
