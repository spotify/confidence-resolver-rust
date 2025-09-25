import {
  ErrorCode,
  EvaluationContext,
  JsonValue,
  Provider,
  ProviderMetadata,
  ProviderStatus,
  ResolutionDetails,
  ResolutionReason,
  OpenFeature
} from '@openfeature/server-sdk';
import { WasmResolver } from './WasmResolver';
import { CachedProvider, Lease, leaseFactory } from './lease';
import { ResolveReason } from './proto/api';

const DEFAULT_STATE_INTERVAL = 10_000;
const DEFAULT_FLUSH_INTERVAL = 10_000;
export interface ProviderOptions {
  clientKey:string,
  clientId:string,
  clientSecret:string,
  stateUpdateInterval?:number,
  flushInterval?:number, 
}

interface AccessToken {
  accessToken: string,
  /// lifetime seconds
  expiresIn: number
}

interface ResolveStateUri {
  signedUri:string, 
  expireTime:string,
  account: string,
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
  status: ProviderStatus = ProviderStatus.NOT_READY;

  private tokenProvider:CachedProvider<string>;
  private stateUriProvider:CachedProvider<{ signedUri:string, account:string }>;
  private flushInterval?:NodeJS.Timeout; 
  private stateInterval?:NodeJS.Timeout;
  private stateEtag:string | null = null;
  

  constructor(private resolver:WasmResolver, private options:ProviderOptions) {
    // TODO better error handling
    // TODO validate options
    this.tokenProvider = leaseFactory(async () => {
      const { accessToken, expiresIn } = await this.fetchToken();
      return [accessToken, new Date(Date.now() + 1000*expiresIn)]
    });
    this.stateUriProvider = leaseFactory(async () => {
      const { signedUri, account, expireTime } = await this.fetchResolveStateUri();
      return [{signedUri, account}, new Date(expireTime)];
    });
  }

  async initialize(context?: EvaluationContext): Promise<void> {
    await this.updateState();
    this.flushInterval = setInterval(() => this.flush(), this.options.flushInterval ?? DEFAULT_FLUSH_INTERVAL);
    if(typeof this.flushInterval.unref === 'function') {
      this.flushInterval.unref();
    } 
    this.stateInterval = setInterval(() => this.updateState(), this.options.stateUpdateInterval ?? DEFAULT_STATE_INTERVAL);
    if(typeof this.stateInterval.unref === 'function') {
      this.stateInterval.unref();
    } 
    this.status = ProviderStatus.READY;
  }

  onClose(): Promise<void> {
    clearInterval(this.flushInterval);
    clearInterval(this.stateInterval);
    return this.flush();
  }

  evaluate<T>(flagKey: string, defaultValue: T, context: EvaluationContext): ResolutionDetails<T> {
    
    const [flagName, ...path] = flagKey.split('.')
    const { resolvedFlags: [flag]} = this.resolver.resolveFlags({
      flags: [`flags/${flagName}`],
      evaluationContext: ConfidenceServerProviderLocal.convertEvaluationContext(context),
      apply: true,
      clientSecret: this.options.clientKey
    });
    if(!flag) {
      return {
        value: defaultValue,
        reason: 'ERROR',
        errorCode: ErrorCode.FLAG_NOT_FOUND
      }
    }
    if(flag.reason != ResolveReason.RESOLVE_REASON_MATCH) {
      return {
        value: defaultValue,
        reason: ConfidenceServerProviderLocal.convertReson(flag.reason),
      }
    }
    let value:unknown = flag.value;
    for(const step of path) {
      if(typeof value !== 'object' || value === null || !hasKey(value, step)) {
        return {
          value: defaultValue,
          reason: 'ERROR',
          errorCode: ErrorCode.TYPE_MISMATCH
        }
      }
      value = value[step];
    }
    if(!valueMatchesSchema(value, defaultValue)) {
      return {
        value: defaultValue,
        reason: 'ERROR',
        errorCode: ErrorCode.TYPE_MISMATCH
      }
    }
    return {
      value,
      reason: 'MATCH',
      variant: flag.variant
    };
  }

  async updateState():Promise<void> {
    const { signedUri, account } = await this.stateUriProvider();
    const req = new Request(signedUri);
    if(this.stateEtag) {
      req.headers.set('If-None-Match', this.stateEtag);
    }
    const resp = await fetch(req);
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

  async flush():Promise<void> {
    const writeFlagLogRequest = this.resolver.flushLogs();
    if(writeFlagLogRequest.length == 0) {
      // nothing to send
      return;
    }
    const resp = await fetch('https://resolver.confidence.dev/v1/flagLogs:write', {
      method: 'post',
      headers: {
        'Content-Type': 'application/x-protobuf',
        'Authorization': `Bearer ${await this.tokenProvider()}`
      },
      body: writeFlagLogRequest as Uint8Array<ArrayBuffer>
    });
    if(resp.ok) {
    } else {
    }
  }

  private async fetchResolveStateUri():Promise<ResolveStateUri> {
    const resp = await fetch('https://flags.confidence.dev/v1/resolverState:resolverStateUri', {
      headers: {
        'Authorization': `Bearer ${await this.tokenProvider()}`
      }
    });
    if(!resp.ok) {
      throw new Error('Failed to get resolve state url');
    }
    return resp.json();
  }

  private async fetchToken():Promise<AccessToken> {
    const resp = await fetch('https://iam.confidence.dev/v1/oauth/token', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        clientId: this.options.clientId,
        clientSecret: this.options.clientSecret,
        grantType: 'client_credentials'
      })
    })
    if(!resp.ok) {
      throw new Error('Failed to fetch access token');
    }
    return resp.json();
  }

  private static convertReson(reason:ResolveReason):ResolutionReason {
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

function valueMatchesSchema<T>(value:unknown, schema:T): value is T {
  if(typeof schema !== typeof value) return false;
  if(typeof value === 'object' && typeof schema === 'object') {
    if(schema === null) return value === null;
    if(Array.isArray(schema)) {
      if(!Array.isArray(value)) return false;
      if(schema.length == 0) return true;
      return value.every(item => valueMatchesSchema(item, schema[0]));
    }
    for(const [key, schemaValue] of Object.entries(schema)) {
      if(!hasKey(value!, key)) return false;
      if(!valueMatchesSchema(value[key], schemaValue)) return false;
    }
  }
  return true;
}

