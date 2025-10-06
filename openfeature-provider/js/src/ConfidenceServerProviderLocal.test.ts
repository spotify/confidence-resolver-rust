import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, MockedFunction, MockedObject, vi } from 'vitest';
import { AccessToken, LocalResolver, ResolveStateUri } from './LocalResolver';
import { ConfidenceServerProviderLocal, DEFAULT_STATE_INTERVAL } from './ConfidenceServerProviderLocal';
import { abortableSleep, TimeUnit } from './util';


const mockedWasmResolver:MockedObject<LocalResolver> = {
  resolveFlags: vi.fn(),
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