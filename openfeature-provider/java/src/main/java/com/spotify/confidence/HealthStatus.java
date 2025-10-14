package com.spotify.confidence;

import static io.grpc.protobuf.services.HealthStatusManager.SERVICE_NAME_ALL_SERVICES;

import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.protobuf.services.HealthStatusManager;
import java.util.concurrent.atomic.AtomicReference;

class HealthStatus {

  private final HealthStatusManager healthStatusManager;
  private final AtomicReference<HealthCheckResponse.ServingStatus> status =
      new AtomicReference<>(HealthCheckResponse.ServingStatus.NOT_SERVING);

  HealthStatus(HealthStatusManager healthStatusManager) {
    this.healthStatusManager = healthStatusManager;
    healthStatusManager.setStatus(SERVICE_NAME_ALL_SERVICES, status.get());
  }

  synchronized void setStatus(HealthCheckResponse.ServingStatus status) {
    this.status.set(status);
    healthStatusManager.setStatus(SERVICE_NAME_ALL_SERVICES, status);
  }
}
