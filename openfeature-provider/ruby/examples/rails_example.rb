#!/usr/bin/env ruby
# frozen_string_literal: true

require "bundler/setup"
require "open_feature/sdk"
require "confidence/openfeature"

# Ruby on Rails integration example for Confidence OpenFeature Provider
#
# This example demonstrates how to use the provider in a Rails-like environment
# Run with: ruby examples/rails_example.rb

puts "=== Confidence OpenFeature Provider - Rails Example ==="
puts

# Simulate Rails environment variable loading
client_secret = ENV["CONFIDENCE_CLIENT_SECRET"] || "CONFIDENCE_CLIENT_SECRET"

# 1. Configuration (like config/initializers/confidence.rb in Rails)
puts "1. Configuring OpenFeature with Confidence provider..."
OpenFeature::SDK.configure do |config|
  api_client = Confidence::OpenFeature::APIClient.new(
    client_secret: client_secret,
    region: Confidence::OpenFeature::Region::EU
  )
  config.set_provider(Confidence::OpenFeature::Provider.new(api_client: api_client))
end
puts "   ✓ Provider configured"
puts

# 2. Simulate a Rails ApplicationController with helper methods
class ApplicationController
  attr_accessor :current_user, :session, :request

  def initialize(current_user: nil, session: {}, request: {})
    @current_user = current_user
    @session = session
    @request = request
  end

  def open_feature_client
    @open_feature_client ||= OpenFeature::SDK.build_client(domain: "rails-app")
  end

  def evaluation_context
    user_id = current_user&.fetch(:id, nil) || session[:id]
    OpenFeature::SDK::EvaluationContext.new(
      targeting_key: user_id,
      user_id: user_id
    )
  end

  def feature_enabled?(flag_key, default: false)
    open_feature_client.fetch_boolean_value(
      flag_key: flag_key,
      default_value: default,
      evaluation_context: evaluation_context
    )
  end
end

# 3. Simulate different user scenarios
puts "2. Testing feature flags with different users..."
puts

premium_user = {id: "user-123", premium: true}
request = {country_code: "SE"}
session = {id: "session-abc"}

controller = ApplicationController.new(
  current_user: premium_user,
  session: session,
  request: request
)

new_ui_enabled = controller.feature_enabled?("mattias-boolean-flag.enabled", default: false)
puts "   New UI enabled: #{new_ui_enabled}"

config = controller.open_feature_client.fetch_object_value(
  flag_key: "mattias-boolean-flag",
  default_value: {},
  evaluation_context: controller.evaluation_context
)
puts "   Full config: #{config}"
puts

puts "=== Rails Integration Tips ==="
puts "1. Add configuration in config/initializers/confidence.rb"
puts "2. Use ENV variables for client_secret (never hardcode)"
puts "3. Create helper methods in ApplicationController"
puts "4. Build evaluation context from current_user and request data"
puts "5. The provider is thread-safe for multi-threaded Rails servers"
puts
puts "Done!"
