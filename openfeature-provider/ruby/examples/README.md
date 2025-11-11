# Confidence OpenFeature Provider - Examples

This directory contains example applications demonstrating how to use the Confidence OpenFeature Provider for Ruby.

## Prerequisites

1. Install dependencies:
   ```bash
   bundle install
   ```

2. Get your Confidence client secret from [Confidence](https://confidence.spotify.com/)

3. Set your client secret as an environment variable:
   ```bash
   export CONFIDENCE_CLIENT_SECRET=your_client_secret_here
   ```

## Examples

### Quick Start (`quick_start.rb`)

A minimal example showing basic flag evaluation for different data types.

**Run it:**
```bash
cd examples
ruby quick_start.rb
```

This example demonstrates:
- Setting up the Confidence provider
- Creating an OpenFeature client
- Evaluating boolean, string, number, and object flags
- Using evaluation context with targeting keys and user attributes

### Ruby on Rails Example (`rails_example.rb`)

A Rails-style integration example showing how to use the provider in a Rails application.

**Run it:**
```bash
cd examples
ruby rails_example.rb
```

Or with your client secret:
```bash
export CONFIDENCE_CLIENT_SECRET=your_client_secret_here
ruby rails_example.rb
```

This example demonstrates:
- Rails initializer-style configuration
- ApplicationController helper methods
- Building evaluation context from user and request data
- Different user scenarios (premium, free, anonymous users)
- Thread-safe usage patterns for Rails servers

### Full Demo App (`demo_app.rb`)

A comprehensive demonstration of all provider features.

**Run it:**
```bash
cd examples
ruby demo_app.rb
```

Or pass the client secret as an argument:
```bash
ruby demo_app.rb your_client_secret_here
```

**Set region (optional):**
```bash
export CONFIDENCE_REGION=US  # or EU (default)
ruby demo_app.rb
```

This example demonstrates:
- Boolean flag evaluation
- String flag evaluation
- Number flag evaluation
- Object flag evaluation
- Using different evaluation contexts
- Targeting based on user attributes

### Advanced Demo (`advanced_demo.rb`)

Real-world use cases and advanced patterns for production applications.

**Run it:**
```bash
cd examples
ruby advanced_demo.rb
```

This example demonstrates:
- Feature rollout strategies (gradual rollout to user segments)
- A/B testing with multiple variants
- Personalization based on user attributes
- Error handling and graceful degradation
- Evaluation without tracking (`apply_on_resolve: false`)
- Custom context builder utilities

## Understanding Flag Keys

Confidence supports dot notation for accessing nested attributes in flags:

- `flag-name.attribute` - Gets a specific attribute from a flag
- `flag-name` - Gets the entire flag value as an object

Example:
```ruby
# Get a specific boolean attribute
enabled = client.fetch_boolean_value(
  flag_key: "feature-toggle.enabled",
  default_value: false,
  evaluation_context: context
)

# Get the entire flag as an object
config = client.fetch_object_value(
  flag_key: "feature-toggle",
  default_value: {},
  evaluation_context: context
)
```

## Evaluation Context

The evaluation context is used for targeting and can include:

```ruby
context = OpenFeature::SDK::EvaluationContext.new(
  targeting_key: "unique-user-id",  # Required for consistent targeting
  attributes: {
    user: {
      country: "SE",
      email: "user@example.com",
      plan: "premium",
      age: 25
    },
    # Add any custom attributes for targeting
    custom_attribute: "value"
  }
)
```

## Regions

The provider supports two regions:
- `Confidence::OpenFeature::Region::EU` (default)
- `Confidence::OpenFeature::Region::US`

Specify the region when creating the API client:

```ruby
api_client = Confidence::OpenFeature::APIClient.new(
  client_secret: client_secret,
  region: Confidence::OpenFeature::Region::US
)
```

## Error Handling

The provider will return default values when:
- A flag is not found
- The flag value doesn't match the expected type
- There's a network error

Example:
```ruby
# Returns false if flag doesn't exist or has wrong type
value = client.fetch_boolean_value(
  flag_key: "non-existent-flag",
  default_value: false,
  evaluation_context: context
)
```

## Next Steps

1. Create flags in your [Confidence](https://confidence.spotify.com/) account
2. Configure targeting rules based on user attributes
3. Test different scenarios with various evaluation contexts
4. Integrate the provider into your application

## Resources

- [Confidence Documentation](https://confidence.spotify.com/docs)
- [OpenFeature Documentation](https://docs.openfeature.dev/)
- [OpenFeature Ruby SDK](https://github.com/open-feature/ruby-sdk)
