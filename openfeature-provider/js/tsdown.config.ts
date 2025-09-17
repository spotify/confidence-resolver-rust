import { defineConfig } from 'tsdown'


const base = defineConfig({
  dts: {
    oxc: true,
  },
  external: ['@bufbuild/protobuf/wire'],
  // inputOptions: {
  //   moduleTypes: {
  //     '.wasm':'asset'
  //   }
  // },
});

export default defineConfig([{
  entry: './src/index.node.ts',
  platform: 'node',
  copy: ['../../wasm/confidence_resolver.wasm'],
  ...base
},{
  entry: './src/index.browser.ts',
  platform: 'browser',
  ...base
}])
