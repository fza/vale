<script module lang="ts" generics="T extends { id: string }">
  // Shared by every instance.
  export const label = 'Shared label';
</script>

<script lang="ts">
  let { items = [], title = 'Untitled list' }: Props<T> = $props();
  let count = $state(0);
</script>

<svelte:head><script type="application/ld+json">{"name": "Structured data"}</script></svelte:head>
<!-- Lists every item once. -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<h1 class="big {active ? 'Active state' : 'idle'}" title={'Attribute value'} {...rest} {name} onclick={() => go('Handler text')}>Visible heading</h1>
{#if items.length > 0 /* Shown when filled. */}
  {#each items as { id, name }, index (id)}<p>{name} {t('Each text')}</p>{:else}<p>Nothing here</p>{/each}
{:else if mode === 'Else-if text'}
{/if}
{#await load('Await text')}…{:then value}{value}{:catch error}{error.message}{/await}
{#key 'Key text'}{/key}
{#snippet row(item: T, note = 'Snippet default')}<td>{item.id}</td>{/snippet}
{@render row(items[0], 'Render text')}
{@html '<b>Raw markup</b>'}
{@const total = sum('Const text')}
{@debug items}
<style lang="scss">
  /* Styles the heading. */

  // Spacing follows the grid.
  .big { background: url(//cdn.example.com/a.png); content: 'Generated content'; }
</style>
