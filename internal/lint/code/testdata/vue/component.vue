<template>
  <div>
    <!-- Kommentar im Template -->
    <p>Bitte die Bestellung prüfen.</p>
    <code><script>// Kein Kommentar.</script></code>
  </div>
</template>

<script lang="ts">// Opens on the tag line.
export default { name: 'OrderView' };
</script>

<script setup lang="ts">
import { ref } from 'vue';

// Holds the current order.
const label = ref('Open the order');
</script>

<style scoped>
/* Hides the footer. */
.footer { content: 'Nicht prüfen'; }
</style>
