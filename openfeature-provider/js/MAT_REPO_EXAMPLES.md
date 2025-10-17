# MaterializationRepository Examples

> **⚠️ WORK IN PROGRESS**
> This feature is currently in development and not yet available in the public API.
> The `MaterializationRepository` interface will allow users to provide their own storage backend for sticky assignment materializations.

This document provides implementation examples for custom `MaterializationRepository` implementations.

## Table of Contents

- [MaterializationRepository Examples](#materializationrepository-examples)
  - [Table of Contents](#table-of-contents)
  - [Basic Usage (Default)](#basic-usage-default)
  - [In-Memory Repository](#in-memory-repository)
  - [File-Backed Repository](#file-backed-repository)
  - [Best Practices](#best-practices)
  - [Other Storage Examples](#other-storage-examples)
    - [Redis Repository](#redis-repository)
    - [Database Repository (Prisma example)](#database-repository-prisma-example)

---

## Basic Usage (Default)

By default, if you don't provide a `materializationRepository`, the provider uses remote storage on Confidence servers:

---

## In-Memory Repository

An in-memory implementation useful for testing, benchmarking, or scenarios where persistence isn't needed:

```typescript
import type { MaterializationRepository, MaterializationInfo } from '@spotify-confidence/openfeature-server-provider-local';

export class InMemoryMaterializationRepo implements MaterializationRepository {
  private storage = new Map<string, Map<string, MaterializationInfo>>();
  private loadCount = 0;
  private storeCount = 0;
  private cacheHits = 0;
  private cacheMisses = 0;

  /**
   * Helper method to create a map with a default, empty MaterializationInfo.
   */
  private createEmptyMap(key: string): Map<string, MaterializationInfo> {
    const emptyInfo: MaterializationInfo = {
      unitInInfo: false,
      ruleToVariant: {}
    };
    return new Map([[key, emptyInfo]]);
  }

  async loadMaterializedAssignmentsForUnit(
    unit: string,
    materialization: string
  ): Promise<Map<string, MaterializationInfo>> {
    this.loadCount++;

    const unitAssignments = this.storage.get(unit);

    if (unitAssignments) {
      if (unitAssignments.has(materialization)) {
        // Cache hit - return only the requested materialization
        const result = new Map<string, MaterializationInfo>();
        result.set(materialization, unitAssignments.get(materialization)!);
        this.cacheHits++;
        return result;
      } else {
        // Materialization not found in cached data for unit
        this.cacheMisses++;
        return this.createEmptyMap(materialization);
      }
    }

    // Cache miss for the unit - return empty map structure
    this.cacheMisses++;
    return this.createEmptyMap(materialization);
  }

  async storeAssignment(
    unit: string,
    assignments: Map<string, MaterializationInfo>
  ): Promise<void> {
    this.storeCount++;

    if (!unit) {
      return;
    }

    // Atomic update: merge new assignments with existing ones
    const existingEntry = this.storage.get(unit);

    if (!existingEntry) {
      // No existing entry - create new one
      this.storage.set(unit, new Map(assignments));
    } else {
      // Merge new assignments into existing entry
      const newEntry = new Map(existingEntry);
      for (const [key, value] of assignments) {
        newEntry.set(key, value);
      }
      this.storage.set(unit, newEntry);
    }
  }

  close(): void {
    this.storage.clear();
  }

  getStats() {
    return {
      units: this.storage.size,
      loads: this.loadCount,
      stores: this.storeCount,
      cacheHits: this.cacheHits,
      cacheMisses: this.cacheMisses
    };
  }

  /**
   * Export all stored data as a plain object for serialization.
   */
  exportData(): Record<string, Record<string, MaterializationInfo>> {
    const result: Record<string, Record<string, MaterializationInfo>> = {};

    for (const [unit, materializations] of this.storage.entries()) {
      result[unit] = {};
      for (const [key, value] of materializations.entries()) {
        result[unit][key] = value;
      }
    }

    return result;
  }
}
```

---

## File-Backed Repository

A file-backed implementation that persists materializations to disk. Should still only be used in a dev environment where a single instance of the server is running:

```typescript
import * as fs from 'fs/promises';
import type { MaterializationRepository, MaterializationInfo } from '@spotify-confidence/openfeature-server-provider-local';

/**
 * File-backed materialization repository.
 *
 * Reads from file on initialization, uses in-memory storage during runtime,
 * and writes back to file on close.
 * Data structure: { [unit: string]: { [materialization: string]: MaterializationInfo } }
 */
export class FileBackedMaterializationRepo implements MaterializationRepository {
  private readonly filePath: string;
  private readonly memoryRepo: InMemoryMaterializationRepo; // From above example

  constructor(filePath: string = './materialization-cache.json') {
    this.filePath = filePath;
    this.memoryRepo = new InMemoryMaterializationRepo();
  }

  /**
   * Initialize by loading data from file into memory.
   */
  async initialize(): Promise<void> {
    try {
      const data = await fs.readFile(this.filePath, 'utf-8');
      const allData: Record<string, Record<string, MaterializationInfo>> = JSON.parse(data);

      // Load all data into the in-memory repo
      for (const [unit, materializations] of Object.entries(allData)) {
        const assignments = new Map<string, MaterializationInfo>();
        for (const [key, value] of Object.entries(materializations)) {
          assignments.set(key, value);
        }
        await this.memoryRepo.storeAssignment(unit, assignments);
      }
    } catch {
      // File doesn't exist or is invalid, start with empty state
    }
  }

  async loadMaterializedAssignmentsForUnit(
    unit: string,
    materialization: string
  ): Promise<Map<string, MaterializationInfo>> {
    return this.memoryRepo.loadMaterializedAssignmentsForUnit(unit, materialization);
  }

  async storeAssignment(
    unit: string,
    assignments: Map<string, MaterializationInfo>
  ): Promise<void> {
    return this.memoryRepo.storeAssignment(unit, assignments);
  }

  async close(): Promise<void> {
    // Export in-memory data and write to file
    const allData = this.memoryRepo.exportData();
    await fs.writeFile(this.filePath, JSON.stringify(allData, null, 2), 'utf-8');
    this.memoryRepo.close();
  }

  getStats() {
    return this.memoryRepo.getStats();
  }
}
```

---

## Best Practices

For production use cases. Always use a single source of truth to be shared by multiple services (multiple SDK instances).

1. **Always call `initialize()`** if your repository needs setup (e.g., loading from disk, connecting to database)

2. **Handle errors gracefully** in `loadMaterializedAssignmentsForUnit`:
   - Return an empty map if data doesn't exist
   - Don't throw exceptions unless there's a critical failure

3. **Make operations atomic** in `storeAssignment`:
   - Merge new assignments with existing ones
   - Use transactions if your storage supports them

4. **Clean up resources** in `close()`:
   - Flush any pending writes
   - Close database connections
   - Clear caches

5. **Consider TTL** for your stored data:
   - Confidence uses 90-day TTL on server-side
   - Implement similar expiration in your repository if needed

6. **Monitor performance**:
   - Track cache hit rates
   - Log slow operations
   - Consider adding metrics

---

## Other Storage Examples

### Redis Repository

```typescript
import type { MaterializationRepository, MaterializationInfo } from '@spotify-confidence/openfeature-server-provider-local';
import { createClient, RedisClientType } from 'redis';

export class RedisMaterializationRepo implements MaterializationRepository {
  private client: RedisClientType;

  constructor(redisUrl: string) {
    this.client = createClient({ url: redisUrl });
  }

  async initialize(): Promise<void> {
    await this.client.connect();
  }

  async loadMaterializedAssignmentsForUnit(
    unit: string,
    materialization: string
  ): Promise<Map<string, MaterializationInfo>> {
    const data = await this.client.get(`unit:${unit}`);
    if (!data) {
      return new Map();
    }
    const parsed = JSON.parse(data);
    return new Map(Object.entries(parsed));
  }

  async storeAssignment(
    unit: string,
    assignments: Map<string, MaterializationInfo>
  ): Promise<void> {
    const serialized = JSON.stringify(Object.fromEntries(assignments));
    // 90-day TTL to match Confidence server behavior
    await this.client.setEx(`unit:${unit}`, 60 * 60 * 24 * 90, serialized);
  }

  async close(): Promise<void> {
    await this.client.quit();
  }
}
```

### Database Repository (Prisma example)

```typescript
import type { MaterializationRepository, MaterializationInfo } from '@spotify-confidence/openfeature-server-provider-local';
import { PrismaClient } from '@prisma/client';

export class DatabaseMaterializationRepo implements MaterializationRepository {
  private prisma: PrismaClient;

  constructor() {
    this.prisma = new PrismaClient();
  }

  async initialize(): Promise<void> {
    await this.prisma.$connect();
  }

  async loadMaterializedAssignmentsForUnit(
    unit: string,
    materialization: string
  ): Promise<Map<string, MaterializationInfo>> {
    const records = await this.prisma.materialization.findMany({
      where: { unit }
    });

    const result = new Map<string, MaterializationInfo>();
    for (const record of records) {
      result.set(record.materializationId, JSON.parse(record.data));
    }
    return result;
  }

  async storeAssignment(
    unit: string,
    assignments: Map<string, MaterializationInfo>
  ): Promise<void> {
    await this.prisma.$transaction(
      Array.from(assignments.entries()).map(([materializationId, info]) =>
        this.prisma.materialization.upsert({
          where: { unit_materializationId: { unit, materializationId } },
          create: {
            unit,
            materializationId,
            data: JSON.stringify(info),
          },
          update: {
            data: JSON.stringify(info),
          },
        })
      )
    );
  }

  async close(): Promise<void> {
    await this.prisma.$disconnect();
  }
}
```
