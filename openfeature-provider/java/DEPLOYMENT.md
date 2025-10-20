# Java Provider Deployment Setup

This document describes the Maven Central deployment setup for the OpenFeature Java Provider.

## Overview

The Java provider is automatically deployed to Maven Central when a release is created via release-please. There are two deployment modes:

1. **Release Deployment**: Triggered when a release PR is merged and a release tag is created
2. **Snapshot Deployment**: Triggered on every push to main (when no release is created)

## Required GitHub Secrets

The following secrets must be configured in the GitHub repository settings:

### Maven Central Credentials

- **`MAVEN_CENTRAL_USERNAME`**: Your Maven Central username/token name
- **`MAVEN_CENTRAL_PASSWORD`**: Your Maven Central password/token

These are used to authenticate with Maven Central's publishing API.

### GPG Signing Key

- **`GPG_PRIVATE_KEY`**: The full GPG private key in ASCII-armored format
  ```bash
  # Export your GPG key:
  gpg --armor --export-secret-keys YOUR_KEY_ID
  ```
- **`SIGN_KEY_PASS`**: The passphrase for the GPG private key

## Workflow Jobs

### `publish-java-provider-release`

Runs when a release is created for the Java provider:
- Checks out the release tag
- Builds the project with Maven
- Generates sources and javadoc JARs
- Signs all artifacts with GPG
- Deploys to Maven Central releases repository
- Auto-publishes the release

### `publish-java-provider-snapshot`

Runs on every push to main when NO release is created:
- Checks out main branch
- Builds the project with Maven
- Generates sources and javadoc JARs  
- Signs all artifacts with GPG
- Deploys to Maven Central snapshots repository

## POM Configuration

The `pom.xml` includes:

1. **Distribution Management**: Defines Maven Central repositories for snapshots and releases
2. **Project Metadata**: License, SCM, developers (required by Maven Central)
3. **Maven Plugins**:
   - `maven-source-plugin`: Generates sources JAR
   - `maven-javadoc-plugin`: Generates javadoc JAR
   - `maven-gpg-plugin`: Signs artifacts with GPG
   - `central-publishing-maven-plugin`: Publishes to Maven Central

## Release Process

1. Changes are committed to main branch
2. Release-please creates/updates a PR with version bumps
3. When PR is merged:
   - If Java provider version changed: release tag is created and **release job** runs
   - If Java provider version unchanged: **snapshot job** runs
4. Artifacts are automatically published to Maven Central

## Testing Deployment

To test the deployment locally (without actually deploying):

```bash
cd openfeature-provider/java
mvn clean install -DskipTests
```

This will build, package, and sign artifacts locally but won't deploy them.

## Troubleshooting

### GPG Signing Issues

If GPG signing fails, ensure:
- The GPG private key is correctly formatted (includes header/footer)
- The passphrase matches the key
- The key hasn't expired

### Maven Central Authentication

If deployment fails with authentication errors:
- Verify credentials are correct in GitHub secrets
- Check that your Maven Central account has publishing permissions
- Ensure the `server-id` in the workflow matches the POM configuration

### Version Conflicts

If Maven Central rejects a version:
- Cannot republish the same version (even after deletion)
- Bump the version and create a new release

