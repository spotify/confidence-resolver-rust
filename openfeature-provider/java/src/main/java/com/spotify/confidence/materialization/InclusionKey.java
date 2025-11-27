package com.spotify.confidence.materialization;

/** Key for checking if a unit is included in a materialization. */
public record InclusionKey(String materialization, String unit) {}

