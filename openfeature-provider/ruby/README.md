# Confidence OpenFeature Ruby Provider

This repo contains the OpenFeature Ruby flag provider for [Confidence](https://confidence.spotify.com/).

## Architecture

**Note:** This provider uses the **online resolver** approach. Flag evaluations are resolved by making API calls to the Confidence backend for each evaluation request. This is different from the local resolver approach used in other Confidence providers (Java, JavaScript, Go) which use WebAssembly (WASM) for local flag evaluation.

## OpenFeature

Before starting to use the provider, it can be helpful to read through the general [OpenFeature docs](https://docs.openfeature.dev/)
and get familiar with the concepts. 

## Support Matrix

This library supports the same platforms as the [OpenFeature Ruby SDK](https://github.com/open-feature/ruby-sdk).

## Installation

Install the gem and add to the application's `Gemfile` by executing:

```sh
bundle add confidence-openfeature-provider
```

If bundler is not being used to manage dependencies, install the gem by executing:

```sh
gem install confidence-openfeature-provider
```

### Creating and using the flag provider

Below is an example for how to create a OpenFeature client using the Confidence flag provider, and then resolve a flag with a boolean attribute. The provider is configured with an api key and a region, which will determine where it will send the resolving requests. 

The flag will be applied immediately, meaning that Confidence will count the targeted user as having received the treatment. 

You can retrieve attributes on the flag variant using property dot notation, meaning `test-flag.boolean-key` will retrieve the attribute `boolean-key` on the flag `test-flag`. 

You can also use only the flag name `test-flag` and retrieve all values as a map with `resolve_object_details()`. 

The flag's schema is validated against the requested data type, and if it doesn't match it will fall back to the default value.

```ruby
require "openfeature/sdk"
require "confidence/openfeature"

# Configure OpenFeature with Confidence provider
OpenFeature::SDK.configure do |config|
  api_client = Confidence::OpenFeature::APIClient.new(
    client_secret: "client_secret",
    region: Confidence::OpenFeature::Region::EU
  )
  config.provider = Confidence::OpenFeature::Provider.new(api_client: api_client)
end

# Create a client
open_feature_client = OpenFeature::SDK.build_client(name: "my-app")

ctx = OpenFeature::SDK::EvaluationContext.new(
  targeting_key: "random",
  attributes: {user: {country: "SE"}}
)

flag_value = open_feature_client.fetch_boolean_value(
  flag_key: "test-flag.boolean-key",
  default_value: false,
  evaluation_context: ctx
)

print(flag_value)
```

### Ruby on Rails Example

For a complete Rails integration example that you can run, see [`examples/rails_example.rb`](examples/rails_example.rb).

Quick snippet:

```ruby
# config/initializers/confidence.rb
require "openfeature/sdk"
require "confidence/openfeature"

OpenFeature::SDK.configure do |config|
  api_client = Confidence::OpenFeature::APIClient.new(
    client_secret: ENV['CONFIDENCE_CLIENT_SECRET'],
    region: Confidence::OpenFeature::Region::EU
  )
  config.provider = Confidence::OpenFeature::Provider.new(api_client: api_client)
end

# app/controllers/application_controller.rb
class ApplicationController < ActionController::Base
  def feature_enabled?(flag_key, default: false)
    client = OpenFeature::SDK.build_client(name: "rails-app")
    ctx = OpenFeature::SDK::EvaluationContext.new(
      targeting_key: current_user&.id || session.id,
      attributes: {user: {country: request.location&.country_code}}
    )
    client.fetch_boolean_value(
      flag_key: flag_key,
      default_value: default,
      evaluation_context: ctx
    )
  end
end
```
