import { ref } from 'vue'

/**
 * How many modal panels are open on the page right now.
 *
 * The dimming belongs to the stack, not to each panel in it: a panel opened
 * over a sheet must not paint a second ground over the first. The count is
 * module state because it is the page's rather than any one dialog's, and it
 * cannot live in `RvDialog.vue` itself — every top-level binding of a
 * `<script setup>` block belongs to one instance of the component.
 */
export const openDialogs = ref(0)
