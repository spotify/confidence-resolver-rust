# Sticky Resolve Documentation

## Overview

Sticky Resolve ensures users receive the same variant throughout an experiment, even if their targeting attributes change or you pause new assignments.

**Two main use cases:**
1. **Consistent experience** - User moves countries but keeps the same variant
2. **Pause intake** - Stop new assignments while maintaining existing ones

**Default behavior:** Sticky assignments are managed by Confidence servers with automatic 90-day TTL. When needed, the provider makes a network call to Confidence. No setup required.

## How It Works

### Default: Server-Side Storage (RemoteResolverFallback)

**Flow:**
1. Local WASM resolver attempts to resolve
2. If sticky data needed → network call to Confidence
3. Confidence checks its sticky repository, returns variant
4. Assignment stored server-side with 90-day TTL (auto-renewed on access)

**Server-side configuration (in Confidence UI):**
- Optionally skip targeting criteria for sticky assignments
- Pause/resume new entity intake
- Automatic TTL management

### Custom: Local Storage (MaterializationRepository)

Implement `MaterializationRepository` to store assignments locally and eliminate network calls.

**Interface:**
```java
public interface MaterializationRepository extends StickyResolveStrategy {
  // Load assignments for a unit (e.g., user ID)
  CompletableFuture<Map<String, MaterializationInfo>> loadMaterializedAssignmentsForUnit(
      String unit, String materialization);

  // Store new assignments
  CompletableFuture<Void> storeAssignment(
      String unit, Map<String, MaterializationInfo> assignments);
}
```

**MaterializationInfo structure:**
```java
record MaterializationInfo(
    boolean isUnitInMaterialization,
    Map<String, String> ruleToVariant  // rule ID -> variant name
)
```

## Implementation Examples

### In-Memory (Testing/Development)

[Here is an example](src/test/java/com/spotify/confidence/InMemoryMaterializationRepoExample.java) on how to implement a simple in-memory `MaterializationRepository`. The same approach can be used with other more persistent storages (like Redis or similar) which is highly recommended for production use cases. 

#### Usage

```java
MaterializationRepository repository = new InMemoryMaterializationRepoExample();

OpenFeatureLocalResolveProvider provider = new OpenFeatureLocalResolveProvider(
    apiSecret,
    clientSecret,
    repository
);
```

## Best Practices

1. **Fail gracefully** - Storage errors shouldn't fail flag resolution
2. **Use 90-day TTL** - Match Confidence's default behavior, renew on read
3. **Connection pooling** - Use pools for Redis/DB connections
4. **Monitor metrics** - Track cache hit rate, storage latency, errors
5. **Test both paths** - Missing assignments (cold start) and existing assignments

## When to Use Custom Storage

| Strategy | Best For | Trade-offs |
|----------|----------|------------|
| **RemoteResolverFallback** (default) | Most apps | Simple, managed by Confidence. Network calls when needed. |
| **MaterializationRepository** (in-memory) | Single-instance apps, testing | Fast, no network. Lost on restart. |
| **MaterializationRepository** (Redis/DB) | Distributed/Multi instance apps | No network calls. Requires storage infra. |

**Start with the default.** Only implement custom storage if you need to eliminate network calls or work offline.

## Additional Resources

- [Confidence Sticky Assignments Documentation](https://confidence.spotify.com/docs/flags/audience#sticky-assignments)
