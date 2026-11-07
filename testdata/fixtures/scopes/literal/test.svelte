<script lang="ts">
// Holds a ZQX for the run.
const label = 'Open the ZQX menu';
</script>

<!-- A ZQX in the markup. -->
<p title={'A ZQX on hover'}>Ein ZQX im Template.</p>
{#if ready}{@render row('Show the ZQX row')}{/if}

<style lang="scss">
// A ZQX in the style.
</style>
