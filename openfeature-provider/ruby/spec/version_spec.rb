require "confidence/openfeature"
require "rspec"

RSpec.describe Confidence::OpenFeature do
  it "has a version number" do
    expect(Confidence::OpenFeature::VERSION).not_to be_nil
  end
end
