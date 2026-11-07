<script context="module">
  /** Loads the page data. */
  export async function preload() {
    return { heading: 'Preloaded heading' };
  }
</script>

<script>
  export let user;
  // Recomputed when the user changes.
  $: greeting = `Hello ${user.name}, welcome back`;
</script>

<div on:click={() => alert('Clicked once')} class:active={user.admin}>
  <slot name="header">Fallback header</slot>
  {#each user.roles as role}<span>{role}</span>{/each}
</div>

<style>
  /* Keeps the card narrow. */
  div { max-width: 40ch; }
</style>
