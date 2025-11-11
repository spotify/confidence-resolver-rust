# frozen_string_literal: true

module Confidence
  module OpenFeature
    class BaseError < StandardError
    end

    class APIError < BaseError
    end

    class FlagNotFoundError < BaseError
    end

    class TypeMismatchError < BaseError
    end
  end
end
