import fs from 'node:fs';
import path from 'node:path';
import { Void } from './proto/messages.ts';
import {
  ResolveFlagsRequest,
  ResolveFlagsResponse,
  ResolveReason
} from './proto/resolver/api.ts';
import { SetResolverStateRequest } from './proto/messages.js';
import { Timestamp } from './proto/google/protobuf/timestamp.ts';
import { ApiBuilder } from './wasm-msg.ts';

const dirname = path.dirname(new URL(import.meta.url).pathname);

// Load the WASM module from shared artifact
const wasmPath = path.join(dirname, '../../confidence_resolver.wasm');
const wasmBuffer = fs.readFileSync(wasmPath);
const resolverState = fs.readFileSync(path.join(dirname, '../../resolver_state.pb'));

// Initialize WASM module
const wasmModule = new WebAssembly.Module(wasmBuffer);

const api = new ApiBuilder()
  .guest('set_resolver_state', SetResolverStateRequest, Void, false)
  .guestRaw('flush_logs')
  .guest('resolve', ResolveFlagsRequest, ResolveFlagsResponse, false)
  .host('current_time', Void, Timestamp, false, () => {
    const now = Date.now();
    return { seconds: Math.floor(now / 1000), nanos: (now % 1000) * 1000000 }
  })
  .build(wasmModule);

api.set_resolver_state({
  state: resolverState,
  accountId: 'confidence-demo-june'
});

// Verify MATCH reason and non-empty variant for tutorial_visitor
{
  const resp = api.resolve({
    clientSecret: 'mkjJruAATQWjeY7foFIWfVAcBWnci2YF',
    apply: true,
    evaluationContext: {
      targeting_key: 'tutorial_visitor',
      visitor_id: 'tutorial_visitor',
    },
    flags: ['flags/tutorial-feature'],
  });
  if (!resp || !resp.resolvedFlags || resp.resolvedFlags.length === 0) {
    throw new Error('No flags resolved for tutorial-feature');
  }
  const rf = resp.resolvedFlags[0];
  if (rf.reason !== ResolveReason.RESOLVE_REASON_MATCH) {
    throw new Error(`Expected reason MATCH, got ${rf.reason}`);
  }
  if (!rf.variant) {
    throw new Error('Expected non-empty variant for tutorial-feature');
  }
  // Extract string title value
  let titleVal: string | null = null;
  const value: any = rf.value as any;
  if (value && typeof value === 'object') {
    if (typeof value.title === 'string') titleVal = value.title;
    else if (typeof value.value === 'string') titleVal = value.value;
    else {
      for (const k of Object.keys(value)) {
        const v = (value as any)[k];
        if (typeof v === 'string') { titleVal = v; break; }
      }
    }
  }
  console.log(`tutorial-feature verified: reason=RESOLVE_REASON_MATCH variant=${rf.variant} title=${titleVal}`);
}
// Done: single flag verified above