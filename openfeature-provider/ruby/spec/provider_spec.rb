require "confidence/openfeature"
require "rspec"

RSpec.describe Confidence::OpenFeature do
  it "has valid regions" do
    expect(Confidence::OpenFeature::Region::EU.uri).not_to be_nil
    expect(Confidence::OpenFeature::Region::US.uri).not_to be_nil
  end

  it "parses valid variants" do
    expect(Confidence::OpenFeature.parse_variant("flags/foo/variants/bar")).to eq("bar")
    expect(Confidence::OpenFeature.parse_variant("flags/foo/variants/bar/baz")).to eq("bar/baz")
  end

  it "fails to parse invalid variants" do
    bad_variants = [
      "a",
      "a/b/c/d",
      "flags/foo/variant/bar",
      "flags/foo",
      "flags/foo/variants"
    ]
    bad_variants.each do |v|
      expect { Confidence::OpenFeature.parse_variant(v) }.to raise_error(ArgumentError)
    end
  end

  describe "the instance" do
    let(:stub_api_client) { StubAPIClient.new }

    subject {
      Confidence::OpenFeature::Provider.new(api_client: stub_api_client)
    }

    it "returns an object" do
      obj = subject.fetch_object_value(flag_key: "foo", default_value: nil)
      expect(obj.value).to eq({"enabled" => true})
      expect(obj.reason).to eq("TARGETING_MATCH")
      expect(obj.variant).to eq("bar")
      expect(stub_api_client.calls.first).to eq(["flags/foo", {}, true])
    end

    it "returns a bool keypath" do
      obj = subject.fetch_boolean_value(flag_key: "foo.enabled", default_value: nil)
      expect(obj.value).to eq(true)
      expect(obj.reason).to eq("TARGETING_MATCH")
      expect(obj.variant).to eq("bar")
      expect(stub_api_client.calls.first).to eq(["flags/foo", {}, true])
    end

    it "propagates context" do
      ctx = FakeEvalCtx.new(targeting_key: "tgt", attributes: {"abc" => "def"})
      subject.fetch_object_value(flag_key: "foo", default_value: nil, evaluation_context: ctx)
      expect(stub_api_client.calls.first).to eq(["flags/foo", {"abc" => "def", "targeting_key" => "tgt"}, true])
    end

    it "fails on incorrect type in keypath" do
      expect {
        subject.fetch_string_value(flag_key: "foo.enabled", default_value: nil)
      }.to raise_error(Confidence::OpenFeature::TypeMismatchError)
    end

    it "fails on invalid keypath" do
      expect {
        subject.fetch_string_value(flag_key: "foo.bar", default_value: nil)
      }.to raise_error(Confidence::OpenFeature::TypeMismatchError)
    end
  end
end

class StubAPIClient
  attr_accessor :calls, :stubs

  def initialize
    @calls = []
    @stubs = {
      "flags/foo" => Confidence::OpenFeature::ResolvedFlag.new(
        flag: "flags/foo",
        variant: "flags/foo/variants/bar",
        value: {"enabled" => true}
      )
    }
  end

  def resolve_one(flag:, context:, apply:)
    @calls << [flag, context, apply]
    @stubs[flag]
  end
end

class FakeEvalCtx
  attr_reader :targeting_key, :attributes

  def initialize(targeting_key:, attributes:)
    @targeting_key = targeting_key
    @attributes = attributes
  end
end
