import log from "loglevel";

export type Fetch = (url:string, init?: RequestInit) => Promise<Response>;

export namespace Fetch {

  export function create(middleware:FetchMiddleware[], sink:Fetch = fetch):Fetch {
    return middleware.reduceRight((next, middleware) => middleware(next), sink);
  }
}

export type FetchMiddleware = (next:Fetch) => Fetch;

export function withTimeout(timeoutMs:number) : FetchMiddleware {
  return next => (url, { signal, ...init } = {}) => {
    
    const timeoutSignal = AbortSignal.timeout(timeoutMs);
    if(signal) {
      signal = AbortSignal.any([signal, timeoutSignal])
    } else {
      signal = timeoutSignal;
    }
    return next(url, { signal, ...init });
  }
}

export function withRetry(opts?:{
  maxAttempts?: number;
  baseDelayMs?: number;
  maxDelayMs?: number;
  backoff?: number; 
  jitter?: number; 
}) : FetchMiddleware {
  const maxAttempts     = opts?.maxAttempts ?? 6;
  const baseDelayMs     = opts?.baseDelayMs ?? 250;
  const maxDelayMs      = opts?.maxDelayMs ?? 30_000;
  const backoff         = opts?.backoff ?? 2;
  const jitter          = opts?.jitter ?? 0.1;
  
  const logger = log.getLogger('withRetry');

  return next => async (url, { body, signal, ...init} = {}) => {
    const cloneBody = await bodyRepeater(body);
    let attempts = 0

    const calculateDelay = ():number => {
      const jitterFactor = 1 + 2 * Math.random() * jitter - jitter;
      return jitterFactor * Math.min(maxDelayMs, baseDelayMs * Math.pow(backoff, attempts - 1));
    }
    const onSuccess = async (resp:Response) => {
      attempts++;
      const { status, statusText } = resp;
      if(status !== 408 && status !== 429 && status < 500 || attempts >= maxAttempts) {
        return resp;
      }
      logger.debug('withRetry %s failed attempt %d with %d %s', url, attempts - 1, status, statusText);
      const serverDelay = parseRetryAfter(resp.headers.get('Retry-After'), baseDelayMs, maxDelayMs);

      await abortableSleep(serverDelay ?? calculateDelay(), signal);
      return doTry();
    }
    const onError = async (error:unknown) => {
      attempts++;
      logger.debug('withRetry %s failed attempt %d with %s', url, attempts - 1, error);
      if(signal?.aborted || attempts >= maxAttempts) {
        throw error;
      }
      await abortableSleep(calculateDelay(), signal);
      return doTry();
    }
    const doTry = ():Promise<Response> => next(url, { body: cloneBody(), signal, ...init}).then(onSuccess, onError);
    
    return doTry().finally(() => {
      logger.debug('withRetry %s finished on attempt %d', url, attempts)
    });
  }
}

export function withAuth(tokenProvider: (fetch:Fetch) => Promise<[token:string, expiry?:Date]>): FetchMiddleware {
  const logger = log.getLogger('withAuth');

  return next => {
    
    let renewTimeout = 0;
    let current:Promise<string> | null = null;
    
    const renewToken = () => {
      logger.debug("withAuth renewing token");
      clearTimeout(renewTimeout);
      current = tokenProvider(next).then(([token, expiry]) => {
        logger.debug("withAuth renew success %s", expiry && (expiry.valueOf() - Date.now()));
        if(expiry) {
          const ttl = expiry.valueOf() - Date.now();
          renewTimeout = setCanonicalTimeout(renewToken, 0.8*ttl);
        }
        return token;
      });
    }

    return async (url, { headers:headersInit, ...init } = {}) => {
      if(!current) {
        renewToken();
      }
      const headers = new Headers(headersInit);
      headers.set('Authorization', `Bearer ${await current}`);
      const currentBeforeFetch = current;
      let resp = await next(url, { headers, ...init });
      if(resp.status === 401) {
        // there might be a race of multiple simultaneous 401
        if(current === currentBeforeFetch) {
          renewToken();
        }
        headers.set('Authorization', `Bearer ${await current}`);
        resp = await next(url, { headers, ...init });
      }
      return resp;
    }
  }
}

export function withRouter(routes:Record<string, FetchMiddleware[]>):FetchMiddleware {
  const logger = log.getLogger('withRouter');
  // Simplified DSL over full URL string:
  // - Anchored regex when pattern starts with ^ and ends with $
  // - '*' alone => match all
  // - Leading single '*' => suffix match
  // - Trailing single '*' => prefix match
  // - Otherwise exact match (any other '*' usage is unsupported)
  const hasOnlyOneStar = (s:string) => (s.split('*').length - 1) === 1;
  const compile = (pattern:string):((url:string)=>boolean) => {
    if(pattern.length >= 2 && pattern[0] === '^' && pattern[pattern.length-1] === '$') {
      const rx = new RegExp(pattern);
      return url => rx.test(url);
    }
    if(pattern === '*') {
      return _ => true;
    }
    if(pattern.startsWith('*') && hasOnlyOneStar(pattern)) {
      const suffix = pattern.slice(1);
      return url => url.endsWith(suffix);
    }
    if(pattern.endsWith('*') && hasOnlyOneStar(pattern)) {
      const prefix = pattern.slice(0, -1);
      return url => url.startsWith(prefix);
    }
    if(pattern.includes('*')) {
      throw new Error(`withRouter unsupported pattern "${pattern}". Only single leading or trailing * (or * alone) supported.`)
    }
    return url => url === pattern;
  };
  const preCompiled = Object.entries(routes)
    .map(([pattern, middlewares]):[(url:string)=>boolean, FetchMiddleware[]] => {
      const predicate = compile(pattern);
      return [predicate, middlewares];
    })
  
  return next => {

    const table = preCompiled
      .map(([predicate, middlewares]):[(url:string)=>boolean, Fetch] => {
        const fetch = Fetch.create(middlewares, next);
        return [predicate, fetch];
      });

    return async (url, init = {}) => {
      const match = table.find(([pred]) => pred(url));
      if(!match) {
        logger.info('withRouter no route matched %s', url);
        return new Response(null, { status: 404 });
      } 
      return match[1](url, init);
    }
  }
}

async function bodyRepeater<T extends BodyInit>(body:BodyInit | null | undefined): Promise<() => T> {
  if(body instanceof ReadableStream) {
    const blob = await new Response(body).blob();
    return () => blob.stream() as T
  }
  return () => body as T;
}

function abortableSleep(millis:number, signal?:AbortSignal | null):Promise<void> {
  return new Promise((resolve, reject) => {
    let timeout:NodeJS.Timeout;
    const onTimeout = () => {
      cleanup();
      resolve();
    }
    const onAbort = () => {
      cleanup();
      reject(signal?.reason);
    }
    const cleanup = () => {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', onAbort);
    }

    if(signal) {
      if(signal.aborted) {
        reject(signal.reason);
        return;
      }
      signal.addEventListener('abort', onAbort);
    }
    timeout = setTimeout(onTimeout, millis);
    if(typeof timeout.unref === 'function') {
      timeout.unref();
    }
  })
}

function setCanonicalTimeout(callb:() => void, millis:number):number {
  let timeout = setTimeout(callb, millis);
  if(typeof timeout === 'object' && timeout !== null && typeof timeout.unref === 'function') {
    timeout.unref();
  }
  return Number(timeout);
}

function parseRetryAfter(retryAfterValue:string | null, min = 0, max = Number.MAX_SAFE_INTEGER):number | undefined {
  if(retryAfterValue) {
    let delay = Number(retryAfterValue)*1000;
    if(Number.isNaN(delay)) {
      delay = Date.parse(retryAfterValue) - Date.now();
    }
    if(Number.isFinite(delay) && delay > 0) {
      return Math.max(min, Math.min(delay, max));
    }
  }
  return undefined;
}