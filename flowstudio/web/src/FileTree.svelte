<script>
  export let nodes = [];
  export let active = '';
  export let onSelect = () => {};
  export let interactive = true;
</script>

<ul class="tree">
  {#each nodes as node (node.path + ':' + node.namespace)}
    <li>
      <button
        class:selected={node.path === active}
        type="button"
        disabled={!interactive}
        title={node.namespace ? `@include AS ${node.namespace}` : node.path}
        on:click={() => interactive && onSelect(node.path)}
      >
        <span class="file-icon">{node.children?.length ? '▾' : '·'}</span>
        <span class="file-name">{node.name}</span>
        {#if node.namespace}<small>{node.namespace}</small>{/if}
      </button>
      {#if node.children?.length}
        <svelte:self nodes={node.children} {active} {onSelect} {interactive} />
      {/if}
    </li>
  {/each}
</ul>

<style>
  .tree { list-style: none; margin: 0; padding: 0 0 0 13px; }
  .tree:first-child { padding-left: 0; }
  li { min-width: 0; }
  button { width: 100%; display: flex; align-items: center; gap: 6px; border: 0; border-radius: 5px; padding: 6px 7px; color: #aab9ce; background: transparent; text-align: left; font-size: 12px; }
  button:not(:disabled) { cursor: pointer; }
  button:hover:not(:disabled), button.selected { color: #e4f1ff; background: #1a2b41; }
  button:disabled { opacity: 1; cursor: default; }
  .file-icon { width: 12px; color: #6689ad; text-align: center; }
  .file-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  small { margin-left: auto; color: #8294ac; font-size: 10px; }
</style>
