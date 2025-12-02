# Contributing

## Development

### Code Formatting

This project uses the [Spotify fmt-maven-plugin](https://github.com/spotify/fmt-maven-plugin) for consistent code formatting.

**Check formatting:**
```bash
mvn fmt:check
```

**Auto-format code:**
```bash
mvn fmt:format
```

The `fmt:check` goal runs automatically during the build to ensure all code is properly formatted.

