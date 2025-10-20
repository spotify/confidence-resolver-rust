import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { OpenFeature } from '@openfeature/server-sdk';
import { ConfidenceServerProviderLocal } from './ConfidenceServerProviderLocal';
import { readFileSync } from 'node:fs';
import { WasmResolver } from './WasmResolver';

const { CONFIDENCE_API_CLIENT_ID, CONFIDENCE_API_CLIENT_SECRET } = requireEnv(
  'CONFIDENCE_API_CLIENT_ID',
  'CONFIDENCE_API_CLIENT_SECRET',
);

const moduleBytes = readFileSync(__dirname + '/../../../wasm/confidence_resolver.wasm');
const module = new WebAssembly.Module(moduleBytes);
const resolver = await WasmResolver.load(module);
const confidenceProvider = new ConfidenceServerProviderLocal(resolver, {
  flagClientSecret: 'RxDVTrXvc6op1XxiQ4OaR31dKbJ39aYV',
  apiClientId: CONFIDENCE_API_CLIENT_ID,
  apiClientSecret: CONFIDENCE_API_CLIENT_SECRET,
});

describe('ConfidenceServerProvider E2E tests', () => {
  beforeAll(async () => {
    await OpenFeature.setProviderAndWait(confidenceProvider);
    OpenFeature.setContext({
      targetingKey: 'test-a', // control
    });
  });

  afterAll(() => OpenFeature.close());

  it('should resolve a boolean e2e', async () => {
    const client = OpenFeature.getClient();

    expect(await client.getBooleanValue('web-sdk-e2e-flag.bool', true)).toBeFalsy();
  });

  it('should resolve an int', async () => {
    const client = OpenFeature.getClient();

    expect(await client.getNumberValue('web-sdk-e2e-flag.int', 10)).toEqual(3);
  });

  it('should resolve a double', async () => {
    const client = OpenFeature.getClient();

    expect(await client.getNumberValue('web-sdk-e2e-flag.double', 10)).toEqual(3.5);
  });

  it('should resolve a string', async () => {
    const client = OpenFeature.getClient();

    expect(await client.getStringValue('web-sdk-e2e-flag.str', 'default')).toEqual('control');
  });

  it('should resolve a struct', async () => {
    const client = OpenFeature.getClient();
    const expectedObject = {
      int: 4,
      str: 'obj control',
      bool: false,
      double: 3.6,
      ['obj-obj']: {},
    };

    expect(await client.getObjectValue('web-sdk-e2e-flag.obj', {})).toEqual(expectedObject);
  });

  it('should resolve a sub value from a struct', async () => {
    const client = OpenFeature.getClient();

    expect(await client.getBooleanValue('web-sdk-e2e-flag.obj.bool', true)).toBeFalsy();
  });

  it('should resolve a sub value from a struct with details with resolve token for client side apply call', async () => {
    const client = OpenFeature.getClient();
    const expectedObject = {
      flagKey: 'web-sdk-e2e-flag.obj.double',
      reason: 'MATCH',
      variant: 'flags/web-sdk-e2e-flag/variants/control',
      flagMetadata: {},
      value: 3.6,
    };

    expect(await client.getNumberDetails('web-sdk-e2e-flag.obj.double', 1)).toEqual(expectedObject);
  });
});

function requireEnv<const N extends string[]>(...names: N): Record<N[number], string> {
  return names.reduce((acc, name) => {
    const value = process.env[name];
    if (!value) throw new Error(`Missing environment variable ${name}`);
    return {
      ...acc,
      [name]: value,
    };
  }, {}) as Record<N[number], string>;
}
