#!/bin/bash
set -euo pipefail
# set e,u,o pipefail so that the script will fail if any command fails
# -e: exit immediately if a command fails
# -u: treat unset variables as an error and exit immediately
# -o pipefail: the return value of a pipeline is the status of the last command to exit with a non-zero status, or zero if no command exited with a non-zero status

CLOUDFLARE_API_TOKEN=${CLOUDFLARE_API_TOKEN:=}
CLOUDFLARE_ACCOUNT_ID=${CLOUDFLARE_ACCOUNT_ID:=}
RESOLVE_TOKEN_ENCRYPTION_KEY=${RESOLVE_TOKEN_ENCRYPTION_KEY:=}
CONFIDENCE_ACCOUNT_ID=${CONFIDENCE_ACCOUNT_ID:=}
CONFIDENCE_RESOLVER_ALLOWED_ORIGIN=${CONFIDENCE_RESOLVER_ALLOWED_ORIGIN:=}
CONFIDENCE_RESOLVER_STATE_URL=${CONFIDENCE_RESOLVER_STATE_URL:=}
CONFIDENCE_RESOLVER_STATE_ETAG_URL=${CONFIDENCE_RESOLVER_STATE_ETAG_URL:=}
CONFIDENCE_CLIENT_ID=${CONFIDENCE_CLIENT_ID:=}
CONFIDENCE_CLIENT_SECRET=${CONFIDENCE_CLIENT_SECRET:=}
NO_DEPLOY=${NO_DEPLOY:=}
FORCE_DEPLOY=${FORCE_DEPLOY:=}

if test -z "$CLOUDFLARE_API_TOKEN"; then
    echo "CLOUDFLARE_API_TOKEN must be set"
    exit 1
fi

if test -z "$RESOLVE_TOKEN_ENCRYPTION_KEY"; then
    echo "RESOLVE_TOKEN_ENCRYPTION_KEY must be set"
    exit 1
fi

if test -z "$CONFIDENCE_ACCOUNT_ID"; then
    echo "CONFIDENCE_ACCOUNT_ID must be set"
    exit 1
fi

if test -z "$CONFIDENCE_RESOLVER_STATE_URL"; then
    # Ensure jq is available for JSON parsing
    if ! command -v jq >/dev/null 2>&1; then
        echo "jq is required but not installed. Please install jq (e.g., brew install jq) or provide CONFIDENCE_RESOLVER_STATE_URL"
        exit 1
    fi

    # Ensure credentials are provided
    if test -z "$CONFIDENCE_CLIENT_ID" || test -z "$CONFIDENCE_CLIENT_SECRET"; then
        echo "CONFIDENCE_CLIENT_ID and CONFIDENCE_CLIENT_SECRET must be set when CONFIDENCE_RESOLVER_STATE_URL is not provided"
        exit 1
    fi

    fetch_access_token() {
        local url="https://iam.confidence.dev/v1/oauth/token"
        local resp http_status body token
        resp=$(curl -s -w "%{http_code}" -H "Content-Type: application/json" \
            -d "{\"clientId\":\"$CONFIDENCE_CLIENT_ID\",\"clientSecret\":\"$CONFIDENCE_CLIENT_SECRET\",\"grantType\":\"client_credentials\"}" \
            "${url}")
        http_status="${resp: -3}"
        body="${resp%???}"
        if [ "$http_status" -eq 200 ] && [ -n "$body" ]; then
            token=$(printf "%s" "$body" | jq -r '.accessToken // .access_token // empty')
            if [ -n "$token" ]; then
                printf "%s" "$token"
                return 0
            fi
        else
            echo "⚠️ Failed to request access token from iam.confidence.dev: HTTP ${http_status}" >&2
        fi
        return 1
    }

    fetch_resolver_state_url() {
        local token
        if ! token=$(fetch_access_token); then
            echo "❌ Unable to obtain access token from IAM API"
            return 1
        fi

        # HTTP using REST transcoding
        local url="https://flags.confidence.dev/v1/resolverState:resolverStateUri"
        local resp
        resp=$(curl -s -w "%{http_code}" -H "Authorization: Bearer ${token}" "${url}")
        local http_status="${resp: -3}"
        local body="${resp%???}"

        if [ "$http_status" -eq 200 ] && [ -n "$body" ]; then
            local signed_uri
            signed_uri=$(printf "%s" "$body" | jq -r '.signedUri // .signed_uri // empty')
            if [ -n "$signed_uri" ]; then
                CONFIDENCE_RESOLVER_STATE_URL="$signed_uri"
                echo "⤵️ Retrieved resolver state URL from flags.confidence.dev"
                return 0
            fi
        else
            echo "⚠️ Failed to fetch resolver state URL from flags.confidence.dev: HTTP ${http_status}" >&2
        fi
        return 1
    }

    if ! fetch_resolver_state_url; then
        echo "❌ Unable to obtain resolver state URL from API. Please set CONFIDENCE_RESOLVER_STATE_URL explicitly"
        exit 1
    fi
fi

mkdir -p data
RESPONSE_FILE="data/resolver_state_current.pb"
ETAG_TOML=""
ALLOWED_ORIGIN_TOML=""
VERSION_TOML=""
CLIENT_ID_TOML=""
CLIENT_SECRET_TOML=""

EXTRA_HEADER=()

# Try to fetch previous etag from deployed resolver endpoint if provided
PREV_ETAG=""
PREV_DEPLOYER_VERSION=""
if [ -n "$CONFIDENCE_RESOLVER_STATE_ETAG_URL" ]; then
    echo "🌐 Fetching etag and git version from $CONFIDENCE_RESOLVER_STATE_ETAG_URL"
    ETAG_BODY_TMP=$(mktemp)
    ETAG_STATUS=$(curl -sS -w "%{http_code}" -o "$ETAG_BODY_TMP" "$CONFIDENCE_RESOLVER_STATE_ETAG_URL") || ETAG_STATUS="000"
    if [ "$ETAG_STATUS" = "200" ]; then
        if command -v jq >/dev/null 2>&1 && grep -q '^[[:space:]]*{' "$ETAG_BODY_TMP"; then
            PREV_ETAG=$(jq -r '.etag // empty' "$ETAG_BODY_TMP") || PREV_ETAG=""
            PREV_DEPLOYER_VERSION=$(jq -r '.version // empty' "$ETAG_BODY_TMP") || PREV_DEPLOYER_VERSION=""
            if [ -n "$PREV_ETAG" ]; then
                echo "⤵️ Previous etag from resolver: $PREV_ETAG"
            else
                echo "⚠️ Resolver returned empty ETag"
            fi
            if [ -n "$PREV_DEPLOYER_VERSION" ]; then
                echo "⤵️ Previous Resolver Version from resolver: $PREV_DEPLOYER_VERSION"
            else
                echo "⚠️ Previous Resolver Version empty from resolver"
            fi
        else
            PREV_ETAG=$(tr -d '\r' < "$ETAG_BODY_TMP")
            PREV_ETAG=$(echo -n "$PREV_ETAG" | tr -d '\n')
            if [ -n "$PREV_ETAG" ]; then
                echo "⤵️ Previous etag from resolver: $PREV_ETAG"
            else
                echo "⚠️ Resolver returned empty ETag"
            fi
        fi
    else
        echo "❌ Could not fetch etag from resolver (HTTP $ETAG_STATUS)"
    fi
    rm -f "$ETAG_BODY_TMP"
fi


DEPLOYER_VERSION=""
if [ -n "${COMMIT_SHA:-}" ]; then
    DEPLOYER_VERSION="$(printf '%s' "$COMMIT_SHA" | tr -d '\n' | cut -c1-12)"
    echo "🔖 Deployer version (env): ${DEPLOYER_VERSION}"
elif command -v git >/dev/null 2>&1; then
    if DEPLOYER_VERSION=$(git rev-parse --short=12 HEAD 2>/dev/null); then
        echo "🐙 Deployer version (commit): ${DEPLOYER_VERSION}"
    else
        echo "❌ git rev-parse failed"
    fi
else
    echo "❌ git not found in PATH and COMMIT_SHA not set"
fi


# If version changed, force download to bypass etag and ensure fresh deploy
if [ -n "$PREV_DEPLOYER_VERSION" ] && [ -n "$DEPLOYER_VERSION" ] && [ "$PREV_DEPLOYER_VERSION" != "$DEPLOYER_VERSION" ]; then
    echo "☑️ Deployer version changed ($PREV_DEPLOYER_VERSION -> $DEPLOYER_VERSION); forcing state download and redeploy"
    FORCE_DEPLOY=1
fi

if [ -n "$PREV_ETAG" ]; then
    if [ -z "$FORCE_DEPLOY" ]; then
        EXTRA_HEADER+=("-H" "If-None-Match: $PREV_ETAG")
        echo "Using If-None-Match: $PREV_ETAG"
    else
        echo "⚠️ FORCE_DEPLOY is set; ignoring existing ETag"
    fi
fi

TMP_HEADER=$(mktemp)
HTTP_STATUS=$(curl -sS -w "%{http_code}" -D "$TMP_HEADER" -o "$RESPONSE_FILE" ${EXTRA_HEADER[@]+"${EXTRA_HEADER[@]}"} "$CONFIDENCE_RESOLVER_STATE_URL")

if [ "$HTTP_STATUS" = "304" ]; then
    echo "✅ Resolver state not modified (HTTP 304). Skipping the deployment"
    # No changes; keep previous ETag
    rm -f "$TMP_HEADER"
    exit 0
elif [ "$HTTP_STATUS" = "200" ]; then
    echo "✅ Download of resolver state successful"
    # Extract etag and normalize
    ETAG_RAW=$(awk -F': ' 'tolower($1)=="etag"{print $2}' "$TMP_HEADER" | tr -d '\r')
    rm -f "$TMP_HEADER"
    # Normalize ETag: drop weak prefix and surrounding quotes, then escape for TOML
    if [ -n "$ETAG_RAW" ]; then
        ETAG_STRIPPED=$(printf '%s' "$ETAG_RAW" | sed -e 's/^W\///' -e 's/^"//' -e 's/"$//')
        ETAG_TOML=$(printf '%s' "$ETAG_STRIPPED" | sed 's/\\/\\\\/g; s/\"/\\\"/g')
    fi
else
    echo "❌ Error downloading resolver state: HTTP status code $HTTP_STATUS"
    # Print response body if the file is not empty
    if [ -s "$RESPONSE_FILE" ]; then
        echo "Server response:"
        cat "$RESPONSE_FILE"
    else
        echo "No response body received"
    fi
    rm -f "$TMP_HEADER"
    exit 1
fi

echo -n "$CONFIDENCE_ACCOUNT_ID" > data/account_id
echo -n "$RESOLVE_TOKEN_ENCRYPTION_KEY" > data/encryption_key

# Function to check if a file exists and is not empty
check_file() {
    if [ ! -s "$1" ]; then
        echo "❌ Error: $1 was not created or is empty!" >&2
        exit 1
    else
        echo "✅ $1 exists and is not empty"
    fi
}

# Verify all required files
check_file "data/resolver_state_current.pb"
check_file "data/account_id"
check_file "data/encryption_key"

echo "🚀 All files successfully created and verified"

cd confidence-cloudflare-resolver

echo "🏁 Starting CloudFlare deployment for $CONFIDENCE_ACCOUNT_ID"
echo "☁️ CloudFlare API token: ${CLOUDFLARE_API_TOKEN:0:5}.."
echo "☁️ CloudFlare account ID: $CLOUDFLARE_ACCOUNT_ID"


if [ -n "$CLOUDFLARE_ACCOUNT_ID" ]; then
    # Remove existing account_id line if present
    sed -i.tmp '/^account_id *= *.*$/d' wrangler.toml
    tmpfile=$(mktemp)
    echo "account_id = \"$CLOUDFLARE_ACCOUNT_ID\"" > "$tmpfile"
    cat wrangler.toml >> "$tmpfile"
    mv "$tmpfile" wrangler.toml
else
    echo "⚠️ CLOUDFLARE_ACCOUNT_ID environment variable is not set. This is required if the CloudFlare API token is of type Account, while User tokens with the correct permissions don't need this env variable set"
fi

# Prepare ALLOWED_ORIGIN for TOML (escape quotes and backslashes)
if [ -n "$CONFIDENCE_RESOLVER_ALLOWED_ORIGIN" ]; then
    ALLOWED_ORIGIN_TOML=$(printf '%s' "$CONFIDENCE_RESOLVER_ALLOWED_ORIGIN" | sed 's/\\/\\\\/g; s/\"/\\\"/g')
fi

# Prepare RESOLVER_VERSION for TOML
if [ -n "$DEPLOYER_VERSION" ]; then
    VERSION_TOML=$(printf '%s' "$DEPLOYER_VERSION" | sed 's/\\/\\\\/g; s/\"/\\\"/g')
fi

# Prepare CONFIDENCE_CLIENT_ID and CONFIDENCE_CLIENT_SECRET for TOML (escape quotes and backslashes)
if [ -n "$CONFIDENCE_CLIENT_ID" ]; then
    CLIENT_ID_TOML=$(printf '%s' "$CONFIDENCE_CLIENT_ID" | sed 's/\\/\\\\/g; s/\"/\\\"/g')
fi
if [ -n "$CONFIDENCE_CLIENT_SECRET" ]; then
    CLIENT_SECRET_TOML=$(printf '%s' "$CONFIDENCE_CLIENT_SECRET" | sed 's/\\/\\\\/g; s/\"/\\\"/g')
fi

# Update [vars] table with ALLOWED_ORIGIN, RESOLVER_STATE_ETAG and RESOLVER_VERSION, without duplicating the table
if [ -n "$ALLOWED_ORIGIN_TOML" ] || [ -n "$ETAG_TOML" ] || [ -n "$DEPLOYER_VERSION" ] || [ -n "$CLIENT_ID_TOML" ] || [ -n "$CLIENT_SECRET_TOML" ]; then
    # Remove any existing definitions to avoid duplicates
    sed -i.tmp '/^ALLOWED_ORIGIN *= *.*$/d' wrangler.toml || true
    sed -i.tmp '/^RESOLVER_STATE_ETAG *= *.*$/d' wrangler.toml || true
    sed -i.tmp '/^RESOLVER_VERSION *= *.*$/d' wrangler.toml || true
    sed -i.tmp '/^DEPLOYER_VERSION *= *.*$/d' wrangler.toml || true
    sed -i.tmp '/^CONFIDENCE_CLIENT_ID *= *.*$/d' wrangler.toml || true
    sed -i.tmp '/^CONFIDENCE_CLIENT_SECRET *= *.*$/d' wrangler.toml || true
    awk -v allowed="${ALLOWED_ORIGIN_TOML}" -v etag="${ETAG_TOML}" -v version="${DEPLOYER_VERSION}" -v client_id="${CLIENT_ID_TOML}" -v client_secret="${CLIENT_SECRET_TOML}" '
        BEGIN{inserted=0}
        {
            print $0
            if (!inserted && $0 ~ /^\[vars\]/) {
                if (allowed != "") print "ALLOWED_ORIGIN = \"" allowed "\""
                if (etag != "") print "RESOLVER_STATE_ETAG = \"" etag "\""
                if (version != "") print "DEPLOYER_VERSION = \"" version "\""
                if (client_id != "") print "CONFIDENCE_CLIENT_ID = \"" client_id "\""
                if (client_secret != "") print "CONFIDENCE_CLIENT_SECRET = \"" client_secret "\""
                inserted=1
            }
        }
    ' wrangler.toml > wrangler.toml.new && mv wrangler.toml.new wrangler.toml
    if [ -n "$ALLOWED_ORIGIN_TOML" ]; then
        echo "✅ ALLOWED_ORIGIN set to \"$CONFIDENCE_RESOLVER_ALLOWED_ORIGIN\" in wrangler.toml"
    fi
    if [ -n "$ETAG_TOML" ]; then
        echo "✅ RESOLVER_STATE_ETAG set to \"$ETAG_TOML\" in wrangler.toml"
    fi
    if [ -n "$DEPLOYER_VERSION" ]; then
        echo "✅ DEPLOYER_VERSION set to \"$DEPLOYER_VERSION\" in wrangler.toml"
    fi
    if [ -n "$CLIENT_ID_TOML" ]; then
        echo "✅ CONFIDENCE_CLIENT_ID set in wrangler.toml"
    fi
    if [ -n "$CLIENT_SECRET_TOML" ]; then
        echo "✅ CONFIDENCE_CLIENT_SECRET set in wrangler.toml"
    fi
fi

# Build the worker after state is downloaded
export CARGO_TARGET_DIR=/workspace/target
RUSTFLAGS='--cfg getrandom_backend="wasm_js"' worker-build --release

# only deploy if NO_DEPLOY is not set
if test -z "$NO_DEPLOY"; then
     # deploy the tail worker
     cd tail
     wrangler deploy
     # deploy the resolver worker
     cd ..
     wrangler deploy
else
     echo "NO_DEPLOY is set, skipping deploy"
fi
