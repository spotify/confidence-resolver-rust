package com.spotify.confidence.materialization;

/** Key identifying a variant assignment within a materialization. */
public record MaterializationKey(String materialization, String unit, String rule) {}

