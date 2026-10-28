'use strict';
import { ref } from 'vue';
export { store } from './store';
declare module 'shims-vue' {}
type Role = 'auditor' | 'admin';
interface Labels { 'title text': string }
enum Mode { Draft = 'Save as draft' }

// Reads the queue once.
const labels = { 'menu label': 'Open the menu', plain: 'Close the menu' };
const pick = labels['menu key'] ?? console.log('Saved the draft');
const lazy = import('./lazy');
throw new Error(`Could not read ${path} for ${user.name}`);
const banner = `Usage: import
  Then check the log.`;
const quote = 'It\'s done';
