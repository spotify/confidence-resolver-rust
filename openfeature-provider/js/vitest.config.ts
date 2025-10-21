import { defineConfig } from 'vitest/config';
import { config, parse } from 'dotenv';
import { existsSync, readFileSync } from 'fs';

export default defineConfig({
  define: {
    __TEST__: 'true',
    __ASSERT__: 'true',
  },
  test: {
    environment: 'node',
    globals: false,
    include: ['src/**/*.{test,spec}.ts'],
    silent: false,
    watch: false,
    env: {
      ...readEnv('.env.test'),
    },
  },
});

function readEnv(file): Record<string, string> {
  try {
    const buf = readFileSync(file);
    return parse(buf);
  } catch (e) {
    if (e.code === 'ENOENT') {
      console.log('could not find', file);
      return {};
    }
    throw e;
  }
}
