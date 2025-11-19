# Confidence OpenFeature Provider Demo (Java)

Demo application showing how to use the Confidence OpenFeature Local Provider in Java.

## Prerequisites

- Java 17+
- Maven 3.8+
- Confidence API credentials

## Setup

1. **Install the Provider Locally**

   Before running the demo, you need to build and install the provider library to your local Maven repository:

   ```bash
   # From openfeature-provider/java directory
   mvn clean install
   ```

2. **Set Environment Variables**

   Set the required environment variables:

   ```bash
   export CONFIDENCE_API_CLIENT_ID="your-api-client-id"
   export CONFIDENCE_API_CLIENT_SECRET="your-api-client-secret"
   export CONFIDENCE_CLIENT_SECRET="your-client-secret"
   ```

   Get your credentials from the [Confidence dashboard](https://confidence.spotify.com/).

## Run

Navigate to the demo directory and run the application:

```bash
cd demo
mvn package
java -jar target/confidence-demo-1.0-SNAPSHOT.jar
```

Alternatively, you can run it using the exec plugin:

```bash
mvn exec:java -Dexec.mainClass="com.spotify.confidence.demo.Main"
```

The demo runs concurrent flag evaluations to test performance and state synchronization.


