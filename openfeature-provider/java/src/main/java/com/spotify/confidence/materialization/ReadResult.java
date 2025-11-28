package com.spotify.confidence.materialization;

import java.util.Optional;

public sealed interface ReadResult permits ReadResult.Inclusion, ReadResult.Variant {
  String materialization();

  String unit();

  // Result for Inclusion
  record Inclusion(String materialization, String unit, boolean included) implements ReadResult {}

  // Result for GetRules
  record Variant(String materialization, String unit, String rule, Optional<String> variant)
      implements ReadResult {}
}
