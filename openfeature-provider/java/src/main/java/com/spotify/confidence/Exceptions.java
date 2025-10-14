package com.spotify.confidence;

public class Exceptions {

  // Internal exceptions - package-private
  static class ValueNotFound extends Exception {
    ValueNotFound(String message) {
      super(message);
    }
  }

  static class IllegalValuePath extends Exception {
    IllegalValuePath(String message) {
      super(message);
    }
  }

  static class ParseError extends RuntimeException {
    ParseError(String message) {
      super(message);
    }
  }

  static class InvalidContextInMessaageError extends RuntimeException {
    InvalidContextInMessaageError(String message) {
      super(message);
    }
  }

  static class IllegalValueType extends Exception {
    IllegalValueType(String message) {
      super(message);
    }
  }

  static class IncompatibleValueType extends Exception {
    IncompatibleValueType(String message) {
      super(message);
    }
  }
}
