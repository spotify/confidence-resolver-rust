package com.spotify.confidence;

import static com.spotify.confidence.JwtUtils.AUTHORIZATION_METADATA_KEY;
import static com.spotify.confidence.JwtUtils.getTokenAsHeader;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ForwardingClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;

/** Client interceptor that attaches a given auth token to all outgoing requests. */
class JwtAuthClientInterceptor implements ClientInterceptor {

  private final TokenHolder tokenHolder;

  public JwtAuthClientInterceptor(TokenHolder tokenHolder) {
    this.tokenHolder = tokenHolder;
  }

  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
    return new ForwardingClientCall.SimpleForwardingClientCall<>(
        next.newCall(method, callOptions)) {
      @Override
      public void start(final Listener<RespT> responseListener, final Metadata headers) {
        headers.put(AUTHORIZATION_METADATA_KEY, getTokenAsHeader(tokenHolder.getToken().token()));
        super.start(responseListener, headers);
      }
    };
  }
}
