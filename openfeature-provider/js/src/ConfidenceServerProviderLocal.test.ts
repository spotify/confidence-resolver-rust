import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, MockedFunction, MockedObject, vi } from 'vitest';
import { AccessToken, LocalResolver, ResolveStateUri } from './LocalResolver';
import { ConfidenceServerProviderLocal, DEFAULT_STATE_INTERVAL } from './ConfidenceServerProviderLocal';
import { abortableSleep, TimeUnit } from './util';
import type { MaterializationRepository } from './MaterializationRepository';
import { RemoteResolverFallback } from './RemoteResolverFallback';
import type { MaterializationInfo, ResolveReason } from './proto/api';


const mockedWasmResolver:MockedObject<LocalResolver> = {
  resolveWithSticky: vi.fn(),
  setResolverState: vi.fn(),
  flushLogs: vi.fn().mockReturnValue(new Uint8Array(100))
}

type Endpoint = (req:Request)=>Promise<Response>
type MockedEndpoint = MockedFunction<Endpoint> & {
  latency: number,
  status: number | string,
  reset: () => void
}

function createEndpointMock(payloadFactory:(req:Request) => BodyInit | null): MockedEndpoint {
  const self = Object.assign(vi.fn(), {
    reset(this:MockedEndpoint) {
      this.latency = 0;
      this.status = 200;
      this.mockImplementation(async (req) => {
        await abortableSleep(this.latency, req.signal);
        if(typeof this.status === 'string') {
          throw new Error(this.status);
        }
        const body = this.status === 200 ? payloadFactory(req) : null;
        return new Response(body, { status: this.status });
      });
    }
  }) as MockedEndpoint;
  return self;
}
const tokenEndpoint = createEndpointMock(() => JSON.stringify({
  accessToken: `<token>`,
  expiresIn: 3600 // 1 hour
} satisfies AccessToken));

const stateUriEndpoint = createEndpointMock(() => JSON.stringify({
  signedUri: 'https://storage.googleapis.com/state',
  account: '<account>'
} satisfies ResolveStateUri))

const stateEndpoint = createEndpointMock(() => new Uint8Array(100));
const flushEndpoint = createEndpointMock(() => null);

const endpointMocks = [tokenEndpoint, stateUriEndpoint, stateEndpoint, flushEndpoint];

const remoteResolveEndpoint = createEndpointMock(() => null);

const mockedFetch:MockedFunction<typeof fetch> = vi.fn(async (input, init) => {
  const req = new Request(input, init);
  switch(req.url) {
    case 'https://iam.confidence.dev/v1/oauth/token':
      return tokenEndpoint(req);
    case 'https://resolver.confidence.dev/v1/flagLogs:write':
      return flushEndpoint(req);
    case 'https://flags.confidence.dev/v1/resolverState:resolverStateUri':
      return stateUriEndpoint(req);
    case 'https://storage.googleapis.com/state':
      return stateEndpoint(req);
    case 'https://resolver.confidence.dev/v1/flags:resolve':
      return remoteResolveEndpoint(req);
  }
  return new Response(null, {
    status: 404,
    statusText: 'Not found'
  })
});

let provider:ConfidenceServerProviderLocal;

vi.useFakeTimers();

beforeEach(() => {
  vi.clearAllMocks();
  vi.clearAllTimers();
  vi.setSystemTime(0);
  provider = new ConfidenceServerProviderLocal(mockedWasmResolver, {
    flagClientSecret:'flagClientSecret',
    apiClientId: 'apiClientId',
    apiClientSecret: 'apiClientSecret',
    fetch: mockedFetch
  });
  endpointMocks.forEach(em => em.reset())

})

afterEach(() => {
})

describe('good conditions', () => {
  it('makes some requests', async () => {
    const asyncAssertions:Promise<any>[] = [];
    
    asyncAssertions.push(
      expect(provider.initialize()).resolves.toBeUndefined()
    );

    await vi.advanceTimersByTimeAsync(TimeUnit.HOUR + TimeUnit.SECOND)

    // the token ttl is one hour, since it renews at 80% of ttl, it will be fetched twice
    expect(tokenEndpoint).toBeCalledTimes(2);
    // since we fetch state every 30s we should fetch 120 times, but we also do an initial fetch in initialize
    expect(stateUriEndpoint).toBeCalledTimes(121);
    expect(stateEndpoint).toBeCalledTimes(121);
    // flush is called every 10s so 360 times in an hour
    expect(flushEndpoint).toBeCalledTimes(360);
    vi.clearAllMocks();

    asyncAssertions.push(
      expect(provider.onClose()).resolves.toBeUndefined()
    );
        
    await vi.runAllTimersAsync();

    // close does a final flush
    expect(flushEndpoint).toBeCalledTimes(1);

    await Promise.all(asyncAssertions);

  })
})

describe('no network', () => {

  beforeEach(() => {
    // stateEndpoint.status = 'No network'
    endpointMocks.forEach(em => {
      em.status = 'No network'
    });
  });

  it('initialize throws after timeout', async () => {
    const asyncAssertions:Promise<any>[] = [];

    asyncAssertions.push(
      expect(provider.initialize()).rejects.toThrow()
    );

    while(provider.status === 'NOT_READY' && Date.now() < TimeUnit.HOUR) {
      await vi.advanceTimersToNextTimerAsync()
    }
    expect(Date.now()).toBe(DEFAULT_STATE_INTERVAL);

    await Promise.all(asyncAssertions);
  })


})

describe('sticky resolve', () => {
  const RESOLVE_REASON_MATCH = 1;

  // TODO: WIP - MaterializationRepository support
  // These tests will be re-enabled when MaterializationRepository is implemented
  describe.skip('with MaterializationRepository strategy', () => {
    let mockRepository: MockedObject<MaterializationRepository>;
    let providerWithRepo: ConfidenceServerProviderLocal;

    beforeEach(async () => {
      mockRepository = {
        loadMaterializedAssignmentsForUnit: vi.fn(),
        storeAssignment: vi.fn(),
        close: vi.fn()
      };

      providerWithRepo = new ConfidenceServerProviderLocal(mockedWasmResolver, {
        flagClientSecret: 'flagClientSecret',
        apiClientId: 'apiClientId',
        apiClientSecret: 'apiClientSecret',
        fetch: mockedFetch,
        materializationRepository: mockRepository
      });

      await providerWithRepo.initialize();
    });

    afterEach(async () => {
      await providerWithRepo.onClose();
    });

    it('should use sticky resolve with repository strategy', async () => {
      mockedWasmResolver.resolveWithSticky.mockReturnValue({
        success: {
          response: {
            resolvedFlags: [{
              flag: 'test-flag',
              variant: 'variant-a',
              value: { color: 'blue' },
              reason: RESOLVE_REASON_MATCH
            }],
            resolveToken: new Uint8Array(),
            resolveId: 'resolve-123'
          },
          updates: []
        }
      });

      const result = await providerWithRepo.evaluate('test-flag.color', 'default', {
        targetingKey: 'user-1'
      });

      expect(result.value).toBe('blue');
      expect(result.reason).toBe('MATCH');
      expect(mockedWasmResolver.resolveWithSticky).toHaveBeenCalledTimes(1);
      expect(mockedWasmResolver.resolveWithSticky).toHaveBeenCalledWith({
        resolveRequest: expect.objectContaining({
          flags: ['flags/test-flag'],
          apply: true,
          clientSecret: 'flagClientSecret'
        }),
        materializationsPerUnit: {},
        failFastOnSticky: false
      });
    });

    it('should store updates from successful resolve', async () => {
      mockedWasmResolver.resolveWithSticky.mockReturnValue({
        success: {
          response: {
            resolvedFlags: [{
              flag: 'test-flag',
              variant: 'variant-a',
              value: { color: 'green' },
              reason: RESOLVE_REASON_MATCH
            }],
            resolveToken: new Uint8Array(),
            resolveId: 'resolve-123'
          },
          updates: [
            {
              unit: 'user-1',
              writeMaterialization: 'mat-v1',
              rule: 'rule-1',
              variant: 'variant-a'
            },
            {
              unit: 'user-1',
              writeMaterialization: 'mat-v2',
              rule: 'rule-2',
              variant: 'variant-b'
            }
          ]
        }
      });

      await providerWithRepo.evaluate('test-flag.color', 'default', {
        targetingKey: 'user-1'
      });

      expect(mockRepository.storeAssignment).toHaveBeenCalledTimes(1);
      expect(mockRepository.storeAssignment).toHaveBeenCalledWith(
        'user-1',
        expect.any(Map)
      );

      const storedMap = mockRepository.storeAssignment.mock.calls[0][1] as Map<string, MaterializationInfo>;
      expect(storedMap.size).toBe(2);
      expect(storedMap.get('mat-v1')).toEqual({
        unitInInfo: true,
        ruleToVariant: { 'rule-1': 'variant-a' }
      });
      expect(storedMap.get('mat-v2')).toEqual({
        unitInInfo: true,
        ruleToVariant: { 'rule-2': 'variant-b' }
      });
    });

    it('should handle missing materializations and retry recursively', async () => {
      // First call: missing materializations
      mockedWasmResolver.resolveWithSticky
        .mockReturnValueOnce({
          missingMaterializations: {
            items: [
              { unit: 'user-1', rule: 'rule-1', readMaterialization: 'mat-v1' }
            ]
          }
        })
        // Second call (retry): success
        .mockReturnValueOnce({
          success: {
            response: {
              resolvedFlags: [{
                flag: 'test-flag',
                variant: 'variant-a',
                value: { color: 'purple' },
                reason: RESOLVE_REASON_MATCH
              }],
              resolveToken: new Uint8Array(),
              resolveId: 'resolve-123'
            },
            updates: []
          }
        });

      mockRepository.loadMaterializedAssignmentsForUnit.mockResolvedValue(
        new Map([
          ['mat-v1', {
            unitInInfo: true,
            ruleToVariant: { 'rule-1': 'variant-a' }
          }]
        ])
      );

      const result = await providerWithRepo.evaluate('test-flag.color', 'default', {
        targetingKey: 'user-1'
      });

      expect(result.value).toBe('purple');
      expect(mockRepository.loadMaterializedAssignmentsForUnit).toHaveBeenCalledWith('user-1', 'mat-v1');
      expect(mockedWasmResolver.resolveWithSticky).toHaveBeenCalledTimes(2);

      // Verify the second call has failFastOnSticky: false
      expect(mockedWasmResolver.resolveWithSticky).toHaveBeenNthCalledWith(2,
        expect.objectContaining({
          failFastOnSticky: false
        })
      );
    });

    it('should handle context without targeting key', async () => {
      // WASM resolver will handle missing targeting key
      mockedWasmResolver.resolveWithSticky.mockReturnValue({
        success: {
          response: {
            resolvedFlags: [{
              flag: 'test-flag',
              variant: 'default-variant',
              value: { setting: 'default' },
              reason: RESOLVE_REASON_MATCH
            }],
            resolveToken: new Uint8Array(),
            resolveId: 'resolve-123'
          },
          updates: []
        }
      });

      const result = await providerWithRepo.evaluate('test-flag.setting', 'fallback', {});

      expect(result.value).toBe('default');
      expect(mockedWasmResolver.resolveWithSticky).toHaveBeenCalled();
    });

    it('should call close on strategy when provider closes', async () => {
      await providerWithRepo.onClose();
      expect(mockRepository.close).toHaveBeenCalledTimes(1);
    });
  });

  describe('remote resolver fallback for sticky assignments', () => {
    let providerWithFallback: ConfidenceServerProviderLocal;

    beforeEach(async () => {
      providerWithFallback = new ConfidenceServerProviderLocal(mockedWasmResolver, {
        flagClientSecret: 'flagClientSecret',
        apiClientId: 'apiClientId',
        apiClientSecret: 'apiClientSecret',
        fetch: mockedFetch
        // Uses remote resolver fallback for sticky assignments
      });

      await providerWithFallback.initialize();
    });

    afterEach(async () => {
      await providerWithFallback.onClose();
    });

    it('should use sticky resolve with fail fast enabled', async () => {
      mockedWasmResolver.resolveWithSticky.mockReturnValue({
        success: {
          response: {
            resolvedFlags: [{
              flag: 'test-flag',
              variant: 'variant-a',
              value: { size: 42 },
              reason: RESOLVE_REASON_MATCH
            }],
            resolveToken: new Uint8Array(),
            resolveId: 'resolve-123'
          },
          updates: []
        }
      });

      const result = await providerWithFallback.evaluate('test-flag.size', 0, {
        targetingKey: 'user-1'
      });

      expect(result.value).toBe(42);
      expect(mockedWasmResolver.resolveWithSticky).toHaveBeenCalledWith({
        resolveRequest: expect.any(Object),
        materializationsPerUnit: {},
        failFastOnSticky: true  // Should be true for fallback strategy
      });
    });

    it('should fallback to remote resolve when materializations missing', async () => {
      mockedWasmResolver.resolveWithSticky.mockReturnValue({
        missingMaterializations: {
          items: [
            { unit: 'user-1', rule: 'rule-1', readMaterialization: 'mat-v1' }
          ]
        }
      });

      remoteResolveEndpoint.mockImplementation(async () => {
        return new Response(JSON.stringify({
          resolvedFlags: [{
            flag: 'test-flag',
            variant: 'variant-b',
            value: { color: 'yellow' },
            reason: 'RESOLVE_REASON_MATCH'
          }],
          resolveToken: '',
          resolveId: 'resolve-456'
        }), { status: 200 });
      });

      const result = await providerWithFallback.evaluate('test-flag.color', 'default', {
        targetingKey: 'user-1'
      });

      expect(result.value).toBe('yellow');
      expect(remoteResolveEndpoint).toHaveBeenCalledTimes(1);
    });

    it('should handle updates with remote fallback (no local storage)', async () => {
      mockedWasmResolver.resolveWithSticky.mockReturnValue({
        success: {
          response: {
            resolvedFlags: [{
              flag: 'test-flag',
              variant: 'variant-a',
              value: { enabled: true },
              reason: RESOLVE_REASON_MATCH
            }],
            resolveToken: new Uint8Array(),
            resolveId: 'resolve-123'
          },
          updates: [
            {
              unit: 'user-1',
              writeMaterialization: 'mat-v1',
              rule: 'rule-1',
              variant: 'variant-a'
            }
          ]
        }
      });

      await providerWithFallback.evaluate('test-flag.enabled', false, {
        targetingKey: 'user-1'
      });

      // Updates are handled by remote resolver, not stored locally
      // Verify no remote resolve was needed (successful local resolve)
      expect(remoteResolveEndpoint).not.toHaveBeenCalled();
    });
  });
})