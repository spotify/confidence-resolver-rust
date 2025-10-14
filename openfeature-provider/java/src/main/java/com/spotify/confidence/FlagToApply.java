package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveTokenV1;
import java.time.Instant;

record FlagToApply(Instant skewAdjustedAppliedTime, ResolveTokenV1.AssignedFlag assignment) {}
