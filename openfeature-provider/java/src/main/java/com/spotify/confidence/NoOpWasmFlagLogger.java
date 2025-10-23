package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsRequest;

/**
 * A no-op implementation of WasmFlagLogger used when flag logging is not needed, typically in test
 * scenarios or when using AccountStateProvider.
 */
class NoOpWasmFlagLogger implements WasmFlagLogger {
  @Override
  public void write(WriteFlagLogsRequest request) {
    // No-op: discard all log requests
  }

  @Override
  public void shutdown() {
    // No-op: nothing to shut down
  }
}

