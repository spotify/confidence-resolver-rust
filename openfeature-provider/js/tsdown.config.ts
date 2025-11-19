import { defineConfig } from 'tsdown';
import { readFileSync } from 'node:fs';

const pkg = JSON.parse(readFileSync(new URL('./package.json', import.meta.url).pathname, 'utf-8'));

const base = defineConfig({
  minify: 'dce-only',
  dts: {
    oxc: true,
  },
  define: {
    __ASSERT__: 'false',
    __TEST__: 'false',
    SDK_VERSION: JSON.stringify(pkg.version),
  },
  external: ['@bufbuild/protobuf/wire'],
  // inputOptions: {
  //   moduleTypes: {
  //     '.wasm':'asset'
  //   }
  // },
});

export default defineConfig([
  {
    entry: './src/index.node.ts',
    platform: 'node',
    copy: ['../../wasm/confidence_resolver.wasm'],
    ...base,
  },
  {
    entry: './src/index.browser.ts',
    platform: 'browser',
    ...base,
  },
]);
