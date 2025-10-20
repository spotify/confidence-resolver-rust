import { vi } from "vitest";
import { AccessToken, ResolveStateUri } from "./LocalResolver";
import { abortableSleep, isObject, TimeUnit } from "./util";
import { ReadableStream as NodeReadableStream } from 'node:stream/web'

type PayloadFactory = (req:Request) => BodyInit | null
type ByteStream = ReadableStream<Uint8Array<ArrayBuffer>>;

const TOKEN_URL = 'https://iam.confidence.dev/v1/oauth/token';
const LOGS_URL = 'https://resolver.confidence.dev/v1/flagLogs:write';
const STATE_URI_URL = 'https://flags.confidence.dev/v1/resolverState:resolverStateUri';
const STATE_URL = 'https://storage.googleapis.com/stateBucket';

type RequestRecord = {
  startTime: number,
  url: string,
  method: string,
  status: number | string,
}

type HandlerFn = (req:Request) => Response | Promise<Response>

type StatusProvider = (req:Request) => number | string

class RequestHandler {
  readonly requests:RequestRecord[] = [];
  latency:number = 0;
  bandwidth:number = Infinity
  error?: string; 
  
  constructor(public handler: HandlerFn) {}

  get calls():number {
    return this.requests.length;
  }

  async handle(req: Request): Promise<Response> {
    const startTime = Date.now()
    let status:number | string = 'UNKNOWN' 
    try {
      await abortableSleep(this.latency/2, req.signal);
      if(this.error) {
        throw Object.assign(new Error(this.error), { name: this.error });
      }
      if(req.body && Number.isFinite(this.bandwidth)) {
        req = new Request(req, { body: throttleStream(req.body, this.latency, req.signal)});
      }
      let resp = await this.handler(req);
      status = resp.status;
      await abortableSleep(this.latency/2, req.signal);
      if(resp.body && Number.isFinite(this.bandwidth)) {
        resp = new Response(throttleStream(resp.body, this.bandwidth, req.signal), {
          status: resp.status,
          statusText: resp.statusText,
          headers: resp.headers,
        })
      }
      return resp;
    }
    catch(err) {
      status = stringifyErrorType(err);
      throw err;
    }
    finally {
      this.requests.push({
        startTime,
        url: req.url,
        method: req.method,
        status
      })
    }
  }
  clear(): void {
    this.requests.length = 0;
  }
}

class RequestDispatcher<T extends RequestHandler> extends RequestHandler {

  constructor(
    private readonly keyFn:(req:Request) => string,
    private readonly dispatchMap:Record<string, T>, 
  ) {
    super(req => {
      const key = this.keyFn(req);
      const handler = this.dispatchMap[key];
      return handler ? handler.handle(req) : new Response(null, { status: 404 });
    })
  }

  clear(): void {
    super.clear();
    return Object.values(this.dispatchMap).forEach(h => h.clear());
  }
}
class EndpointMock extends RequestHandler {
  
  status: number | string | StatusProvider = 200;

  constructor(private payloadFactory: PayloadFactory = () => null) {
    super(req => {
      let status = this.status;
      if(typeof status === 'function') {
        status = status(req);
      }
      if(typeof status === 'string') {
        throw new Error(status);
      }
      if(status === 200) {
        return new Response(this.payloadFactory(req))
      }
      return new Response(null, { status })  
    })
  }

}

class ServerMock extends RequestDispatcher<EndpointMock> {

  constructor(private endpoints:Record<string,EndpointMock>) {
    super(req => new URL(req.url).pathname, endpoints)
  }

}

class IamServerMock extends ServerMock {

  readonly token: EndpointMock;

  constructor() {
    let nextToken = 1
    const tokenEndpoint = new EndpointMock(() => JSON.stringify({
      accessToken: `token${nextToken++}`,
      expiresIn: 60*60
    } satisfies AccessToken));
    super({ '/v1/oauth/token': tokenEndpoint })
    this.token = tokenEndpoint;
  }
}

class FlagsServerMock extends ServerMock {

  readonly stateUri: EndpointMock;

  constructor() {
    const stateUri = new EndpointMock(() => JSON.stringify({
      signedUri: STATE_URL,
      account: '<account>'
    } satisfies ResolveStateUri));
    super({'/v1/resolverState:resolverStateUri': stateUri })
    this.stateUri = stateUri;
  }
}
class ResolverServerMock extends ServerMock {

  readonly flagLogs: EndpointMock

  constructor() {
    const flagLogs = new EndpointMock();
    super({
      '/v1/flagLogs:write': flagLogs
    })
    this.flagLogs = flagLogs;
  }
}

class GcsServerMock extends ServerMock {

  readonly stateBucket:EndpointMock;
  constructor() {
    const stateBucket = new EndpointMock(() => new ArrayBuffer(100))
    super({
      '/stateBucket': stateBucket
    })
    this.stateBucket = stateBucket;
  }
}

export class NetworkMock extends RequestDispatcher<ServerMock> {

  readonly iam: IamServerMock;
  readonly flags: FlagsServerMock;
  readonly resolver: ResolverServerMock;
  readonly gcs: GcsServerMock;

  constructor() {
    const iam = new IamServerMock();
    const flags = new FlagsServerMock();
    const resolver = new ResolverServerMock();
    const gcs = new GcsServerMock();

    super(
      req => new URL(req.url).hostname, 
      {
        'iam.confidence.dev': iam,
        'flags.confidence.dev': flags,
        'resolver.confidence.dev': resolver,
        'storage.googleapis.com': gcs,
      });
    this.iam = iam;
    this.flags = flags;
    this.resolver = resolver;
    this.gcs = gcs;
  }

  readonly fetch:typeof fetch = (input, init) => this.handle(new Request(input, init))
}

function throttleStream(stream:ByteStream, bandwidth:number, signal?:AbortSignal):ByteStream {
  const iter = (async function*() {
    for await(const chunk of stream) {
      await abortableSleep(chunk.length/bandwidth*1000, signal);
      yield chunk;
    }
  })();
  return NodeReadableStream.from(iter) as ByteStream;
}

function stringifyErrorType(err:unknown):string {
  if(isObject(err) && 'name' in err && typeof err.name === 'string') {
    return err.name;
  }
  return String(err);
}


if(vi.isFakeTimers()) {
  throw new Error('FakeTimers should not be on when test-helpers.ts is loaded!');
}
const realSetImmediate = setImmediate;


export async function advanceTimersUntil(predicate:() => boolean):Promise<void>
export async function advanceTimersUntil(opt: { timeout?: number }, predicate:() => boolean):Promise<void>
export async function advanceTimersUntil<T>(promise:Promise<T>):Promise<T>
export async function advanceTimersUntil<T>(opt: { timeout: number }, promise:Promise<T>):Promise<T>
export async function advanceTimersUntil(...args:any[]):Promise<any> {
  if(!vi.isFakeTimers()) {
    throw new Error('FakeTimers are not enabled');
  }
  const opt: { timeout?: number } = args.length == 2 ? args.shift() : {}; 
  
  let predicate: () => boolean;
  let ret = undefined;
  if(typeof args[0] === 'function') {
    predicate = args[0];
  } else {
    let done = false;
    ret = args[0];
    ret.finally(() => { done = true; })
    predicate = () => done;
  }
  
  if(opt.timeout) {
    let timedOut = false;
    const timeout = setTimeout(() => { timedOut = true; }, opt.timeout);
    const origPred = predicate;
    predicate = () => {
      if(timedOut) {
        throw new Error('advanceTimersUntil: Timed out');
      }
      try {
        if(origPred()) {
          clearTimeout(timeout);
          return true;
        }
        return false;
      }
      catch(err) {
        clearTimeout(timeout);
        throw err;
      }
    }
  }

  await new Promise(resolve => { realSetImmediate(resolve); });

  while(!predicate()) {
    // some code, notably NodeJS WHATWG streams and fetch impl. might schedule immediate calls
    // that isn't mocked by fake timers, so we advance that first.
    if(process.getActiveResourcesInfo().includes('Immediate')) {
      await new Promise(resolve => { realSetImmediate(resolve); });
      continue;
    }
    if(vi.getTimerCount() === 0) {
      throw new Error('advanceTimersUntil: Condition not met and no timers left to advance')
    }
    await vi.advanceTimersToNextTimerAsync();
  }
  return ret;
}

