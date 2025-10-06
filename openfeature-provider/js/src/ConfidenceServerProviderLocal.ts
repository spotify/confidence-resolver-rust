import type {
  ErrorCode,
  EvaluationContext,
  JsonValue,
  Provider,
  ProviderMetadata,
  ProviderStatus,
  ResolutionDetails,
  ResolutionReason,
} from '@openfeature/server-sdk';
import { ResolveReason } from './proto/api';
import { Fetch, FetchMiddleware, withAuth, withLogging, withResponse, withRetry, withRouter, withStallTimeout, withTimeout } from './fetch';
import { scheduleWithFixedInterval, timeoutSignal, TimeUnit } from './util';
import { AccessToken, LocalResolver, ResolveStateUri } from './LocalResolver';

export const DEFAULT_STATE_INTERVAL = 30_000;
export const DEFAULT_FLUSH_INTERVAL = 10_000;
export interface ProviderOptions {
  flagClientSecret:string,
  apiClientId:string,
  apiClientSecret:string,
  initializeTimeout?:number,
  flushInterval?:number, 
  fetch?: typeof fetch,
}

/**
 * OpenFeature Provider for Confidence Server SDK (Local Mode)
 * @public
 */
export class ConfidenceServerProviderLocal implements Provider {
  /** Static data about the provider */
  readonly metadata: ProviderMetadata = {
    name: 'ConfidenceServerProviderLocal',
  };
  /** Current status of the provider. Can be READY, NOT_READY, ERROR, STALE and FATAL. */
  status = 'NOT_READY' as ProviderStatus;

  private readonly main = new AbortController();
  private readonly fetch:Fetch;
  private readonly flushInterval:number;
  private stateEtag:string | null = null;
  

  // TODO Maybe pass in a resolver factory, so that we can initialize it in initialize and transition to fatal if not.
  constructor(private resolver:LocalResolver, private options:ProviderOptions) {
    // TODO better error handling
    // TODO validate options
    this.flushInterval = options.flushInterval ?? DEFAULT_FLUSH_INTERVAL;
    const withConfidenceAuth = withAuth(async () => {
      const { accessToken, expiresIn } = await this.fetchToken();
      return [accessToken, new Date(Date.now() + 1000*expiresIn)]
    }, this.main.signal);

    const withFastRetry = FetchMiddleware.compose(
      withRetry({
        maxAttempts: Infinity,
        baseInterval: 300,
        maxInterval: 5*TimeUnit.SECOND
      }),
      withTimeout(5*TimeUnit.SECOND)
    );

    this.fetch = Fetch.create([
      withRouter({
        'https://iam.confidence.dev/v1/oauth/token': [
          withFastRetry
        ],
        'https://storage.googleapis.com/*':[
          withRetry({
            maxAttempts:  Infinity,
            baseInterval:  500,
            maxInterval:   DEFAULT_STATE_INTERVAL,
          }),
          withStallTimeout(500)
        ],
        'https://flags.confidence.dev/*|https://resolver.confidence.dev/*':[
          withConfidenceAuth,
          withRouter({
            '*/v1/resolverState:resolverStateUri':[
              withFastRetry,    
            ],
            '*/v1/flagLogs:write':[
              withRetry({
                maxAttempts: 3,
                baseInterval: 500,
              }),
              withTimeout(5*TimeUnit.SECOND)
            ]
          }),
        ],
        // non-configured requests
        '*': [withResponse((url) => { throw new Error(`Unknown route ${url}`)})]
      }),
      withLogging()
    ], options.fetch ?? fetch);
  }

  async initialize(context?: EvaluationContext): Promise<void> {
    // TODO validate options and switch to fatal.
    const signal = this.main.signal;
    const initialUpdateSignal = AbortSignal.any([signal, timeoutSignal(this.options.initializeTimeout ?? DEFAULT_STATE_INTERVAL)]); 
    try {
      // TODO set schedulers irrespective of failure
      // TODO if 403 here, 
      await this.updateState(initialUpdateSignal);
      scheduleWithFixedInterval(signal => this.flush(signal), this.flushInterval, { maxConcurrent: 3, signal });
      // TODO Better with fixed delay so we don't do a double fetch when we're behind. Alt, skip if in progress
      scheduleWithFixedInterval(signal => this.updateState(signal), DEFAULT_STATE_INTERVAL, { signal });
      this.status = 'READY' as ProviderStatus;
    } catch(e:unknown) {
      this.status = 'ERROR' as ProviderStatus;
      // TODO should we swallow this?
      throw e;
    }
  }

  onClose(): Promise<void> {
    this.main.abort();
    return this.flush();
  }

  // TODO test unknown flagClientSecret
  evaluate<T>(flagKey: string, defaultValue: T, context: EvaluationContext): ResolutionDetails<T> {
    
    const [flagName, ...path] = flagKey.split('.')
    const { resolvedFlags: [flag]} = this.resolver.resolveFlags({
      flags: [`flags/${flagName}`],
      evaluationContext: ConfidenceServerProviderLocal.convertEvaluationContext(context),
      apply: true,
      clientSecret: this.options.flagClientSecret
    });
    if(!flag) {
      return {
        value: defaultValue,
        reason: 'ERROR',
        errorCode: 'FLAG_NOT_FOUND' as ErrorCode
      }
    }
    if(flag.reason != ResolveReason.RESOLVE_REASON_MATCH) {
      return {
        value: defaultValue,
        reason: ConfidenceServerProviderLocal.convertReason(flag.reason),
      }
    }
    let value:unknown = flag.value;
    for(const step of path) {
      if(typeof value !== 'object' || value === null || !hasKey(value, step)) {
        return {
          value: defaultValue,
          reason: 'ERROR',
          errorCode: 'TYPE_MISMATCH' as ErrorCode
        }
      }
      value = value[step];
    }
    if(!isAssignableTo(value, defaultValue)) {
      return {
        value: defaultValue,
        reason: 'ERROR',
        errorCode: 'TYPE_MISMATCH' as ErrorCode
      }
    }
    return {
      value,
      reason: 'MATCH',
      variant: flag.variant
    };
  }

  async updateState(signal?:AbortSignal):Promise<void> {
    const { signedUri, account } = await this.fetchResolveStateUri(signal);
    const headers = new Headers()
    if(this.stateEtag) {
      headers.set('If-None-Match', this.stateEtag);
    }
    const resp = await this.fetch(signedUri, { headers, signal });
    if(resp.status === 304) {
      // not changed
      return;
    }
    if(!resp.ok) {
      throw new Error(`Failed to fetch state: ${resp.status} ${resp.statusText}`);
    }
    this.stateEtag = resp.headers.get('etag');
    const state = new Uint8Array(await resp.arrayBuffer());
    this.resolver.setResolverState({
      accountId: account,
      state
    })
  }

  async flush(signal?:AbortSignal):Promise<void> {
    const writeFlagLogRequest = this.resolver.flushLogs();
    if(writeFlagLogRequest.length == 0) {
      // nothing to send
      return;
    }
    await this.fetch('https://resolver.confidence.dev/v1/flagLogs:write', {
      method: 'post',
      signal,
      headers: {
        'Content-Type': 'application/x-protobuf',
      },
      body: writeFlagLogRequest as Uint8Array<ArrayBuffer>
    });
  }

  private async fetchResolveStateUri(signal?: AbortSignal):Promise<ResolveStateUri> {
    const resp = await this.fetch('https://flags.confidence.dev/v1/resolverState:resolverStateUri', { signal });
    if(!resp.ok) {
      throw new Error('Failed to get resolve state url');
    }
    return resp.json();
  }

  private async fetchToken():Promise<AccessToken> {
    const resp = await this.fetch('https://iam.confidence.dev/v1/oauth/token', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        clientId: this.options.apiClientId,
        clientSecret: this.options.apiClientSecret,
        grantType: 'client_credentials'
      })
    })
    if(!resp.ok) {
      throw new Error('Failed to fetch access token');
    }
    return resp.json();
  }

  private static convertReason(reason:ResolveReason):ResolutionReason {
    switch(reason) {
      case ResolveReason.RESOLVE_REASON_ERROR:
        return 'ERROR';
      case ResolveReason.RESOLVE_REASON_FLAG_ARCHIVED:
        return 'FLAG_ARCHIVED';
      case ResolveReason.RESOLVE_REASON_MATCH:
        return 'MATCH';
      case ResolveReason.RESOLVE_REASON_NO_SEGMENT_MATCH:
        return 'NO_SEGMENT_MATCH';
      case ResolveReason.RESOLVE_REASON_TARGETING_KEY_ERROR:
        return 'TARGETING_KEY_ERROR';
      case ResolveReason.RESOLVE_REASON_NO_TREATMENT_MATCH:
        return 'NO_TREATMENT_MATCH';
      default:
        return 'UNSPECIFIED'
    }
  }

  private static convertEvaluationContext({ targetingKey:targeting_key, ...rest}:EvaluationContext): { [key: string]: any } {
    return {
      targeting_key, ...rest
    }
  }

  /** Resolves with an evaluation of a Boolean flag */
  resolveBooleanEvaluation(
    flagKey: string,
    defaultValue: boolean,
    context: EvaluationContext,
  ): Promise<ResolutionDetails<boolean>> {
    return Promise.resolve(this.evaluate(flagKey, defaultValue, context));
  }
  /** Resolves with an evaluation of a Numbers flag */
  resolveNumberEvaluation(
    flagKey: string,
    defaultValue: number,
    context: EvaluationContext,
  ): Promise<ResolutionDetails<number>> {
    return Promise.resolve(this.evaluate(flagKey, defaultValue, context));
  }
  /** Resolves with an evaluation of an Object flag */
  resolveObjectEvaluation<T extends JsonValue>(
    flagKey: string,
    defaultValue: T,
    context: EvaluationContext,
  ): Promise<ResolutionDetails<T>> {
    return Promise.resolve(this.evaluate(flagKey, defaultValue, context));
  }
  /** Resolves with an evaluation of a String flag */
  resolveStringEvaluation(
    flagKey: string,
    defaultValue: string,
    context: EvaluationContext,
  ): Promise<ResolutionDetails<string>> {
    return Promise.resolve(this.evaluate(flagKey, defaultValue, context));
  }
}

function hasKey<K extends string>(obj:object, key:K): obj is { [P in K]: unknown } {
  return key in obj;
}

function isAssignableTo<T>(value:unknown, schema:T): value is T {
  if(typeof schema !== typeof value) return false;
  if(typeof value === 'object' && typeof schema === 'object') {
    if(schema === null) return value === null;
    if(Array.isArray(schema)) {
      if(!Array.isArray(value)) return false;
      if(schema.length == 0) return true;
      return value.every(item => isAssignableTo(item, schema[0]));
    }
    for(const [key, schemaValue] of Object.entries(schema)) {
      if(!hasKey(value!, key)) return false;
      if(!isAssignableTo(value[key], schemaValue)) return false;
    }
  }
  return true;
}

