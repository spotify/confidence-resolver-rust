import type { ResolveWithStickyRequest, ResolveWithStickyResponse, SetResolverStateRequest } from './proto/api';

export interface LocalResolver {
  resolveWithSticky(request: ResolveWithStickyRequest): ResolveWithStickyResponse;
  setResolverState(request: SetResolverStateRequest): void;
  flushLogs(): Uint8Array;
  flushAssigned(): Uint8Array;
}

export interface AccessToken {
  accessToken: string;
  /// lifetime seconds
  expiresIn: number;
}

export interface ResolveStateUri {
  signedUri: string;
  account: string;
}
