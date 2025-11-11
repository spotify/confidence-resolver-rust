#!/usr/bin/env ruby
# frozen_string_literal: true

require "bundler/setup"
require "open_feature/sdk"
require "confidence/openfeature"

# Quick start example for Confidence OpenFeature Provider

# Get client secret from environment variable
client_secret = ENV["CONFIDENCE_CLIENT_SECRET"] || "CONFIDENCE_CLIENT_SECRET"

# Configure OpenFeature with Confidence provider
OpenFeature::SDK.configure do |config|
  api_client = Confidence::OpenFeature::APIClient.new(
    client_secret: client_secret,
    region: Confidence::OpenFeature::Region::EU
  )
  config.set_provider(Confidence::OpenFeature::Provider.new(api_client: api_client))
end

# Create a client
client = OpenFeature::SDK.build_client(domain: "quick-start-app")

# Create evaluation context with user information
context = OpenFeature::SDK::EvaluationContext.new(
  user_id: "user-123"
)

# Evaluate a boolean flag
puts "Evaluating boolean flag..."
enabled = client.fetch_boolean_value(
  flag_key: "mattias-boolean-flag.enabled",
  default_value: false,
  evaluation_context: context
)
puts "Feature enabled: #{enabled}"

# Evaluate an object flag
puts "\nEvaluating object flag..."
config_obj = client.fetch_object_value(
  flag_key: "mattias-boolean-flag",
  default_value: {},
  evaluation_context: context
)
puts "Config: #{config_obj}"

puts "\nDone!"
