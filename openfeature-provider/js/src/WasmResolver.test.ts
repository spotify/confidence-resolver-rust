import { beforeEach, describe, expect, it, test } from 'vitest';
import { WasmResolver } from './WasmResolver';
import { readFileSync } from 'node:fs';
import { ResolveReason } from './proto/api';
import { spawnSync } from 'node:child_process';
import { error } from 'node:console';
import { stderr } from 'node:process';

const moduleBytes = readFileSync(__dirname + '/../../../wasm/confidence_resolver.wasm');
const stateBytes = readFileSync(__dirname + '/../../../wasm/resolver_state.pb');

const CLIENT_SECRET = 'mkjJruAATQWjeY7foFIWfVAcBWnci2YF';

let wasmResolver: WasmResolver;
beforeEach(async () => {
  wasmResolver = await WasmResolver.load(new WebAssembly.Module(moduleBytes));
});

it('should fail to resolve without state', () => {
  expect(() => {
    wasmResolver.resolveWithSticky({
      resolveRequest: { flags: [], clientSecret: 'xyz', apply: false },
      materializationsPerUnit: {},
      failFastOnSticky: false
    });
  }).toThrowError('Resolver state not set');
});

describe('with state', () => {
  beforeEach(() => {
    wasmResolver.setResolverState({ state: stateBytes, accountId: 'confidence-test' });
  });

  it('should resolve flags', () => {
    try {
      const resp = wasmResolver.resolveWithSticky({
        resolveRequest: {
          flags: ['flags/tutorial-feature'],
          clientSecret: CLIENT_SECRET,
          apply: true,
          evaluationContext: {
            targeting_key: 'tutorial_visitor',
            visitor_id: 'tutorial_visitor',
          },
        },
        materializationsPerUnit: {},
        failFastOnSticky: false
      });

      expect(resp.success).toBeDefined();
      expect(resp.success?.response).toMatchObject({
        resolvedFlags: [
          {
            reason: ResolveReason.RESOLVE_REASON_MATCH,
          },
        ],
      });
    } catch (e) {
      console.log('yo', e);
    }
  });

  describe('flushLogs', () => {

    it('should be empty before any resolve', () => {
      const logs = wasmResolver.flushLogs();
      expect(logs.length).toBe(0);
    })

    it('should contain logs after a resolve', () => {
      wasmResolver.resolveWithSticky({
        resolveRequest: {
          flags: ['flags/tutorial-feature'],
          clientSecret: CLIENT_SECRET,
          apply: true,
          evaluationContext: {
            targeting_key: 'tutorial_visitor',
            visitor_id: 'tutorial_visitor',
          },
        },
        materializationsPerUnit: {},
        failFastOnSticky: false
      });

      const decoded = decodeBuffer(wasmResolver.flushLogs());

      expect(decoded).contains('flag_assigned');
      expect(decoded).contains('client_resolve_info');
      expect(decoded).contains('flag_resolve_info');
    })
  })
});


function decodeBuffer(input:Uint8Array):string {
  const res = spawnSync('protoc',[
    `-I${__dirname}/../../../confidence-resolver/protos`,
    `--decode=confidence.flags.resolver.v1.WriteFlagLogsRequest`, 
    `confidence/flags/resolver/v1/internal_api.proto`
  ], { input, encoding: 'utf8' });
  if(res.error) {
    throw res.error;
  }
  if(res.status !== 0) {
    throw new Error(res.stderr)
  }
  return res.stdout;
}