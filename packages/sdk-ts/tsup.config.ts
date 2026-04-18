import { defineConfig } from 'tsup';

export default defineConfig([
  {
    entry: {
      index: 'src/index.ts',
    },
    format: ['esm', 'cjs'],
    dts: true,
    sourcemap: true,
    clean: true,
    splitting: false,
    target: 'es2020',
    // Keep bundle tree-shakeable for consumers.
    treeshake: true,
    // The browser entry MUST NOT pull in ws — the log-stream subpath does.
    external: ['ws'],
  },
  {
    entry: {
      node: 'src/node.ts',
    },
    format: ['esm', 'cjs'],
    dts: true,
    sourcemap: true,
    clean: false,
    target: 'es2020',
    platform: 'node',
  },
]);
