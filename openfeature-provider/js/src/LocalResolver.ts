import type { ResolveWithStickyRequest, ResolveWithStickyResponse, SetResolverStateRequest } from './proto/api';

export interface LocalResolver {
  resolveWithSticky(request: ResolveWithStickyRequest): ResolveWithStickyResponse;
  setResolverState(request: SetResolverStateRequest): void;
  flushLogs(): Uint8Array;
}
