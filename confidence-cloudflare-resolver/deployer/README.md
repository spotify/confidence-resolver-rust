# CloudFlare Resolver Worker Deployer

Docker container used to deploy the Confidence Rust resolver to CloudFlare.

# Build the image

From the **root of the repository**, run:

```
docker build -f confidence-cloudflare-resolver/deployer/Dockerfile -t <YOUR_IMAGE_NAME> .
```

# Usage

```
docker run -it \
	-e CLOUDFLARE_API_TOKEN='<>’ \
	-e CONFIDENCE_ACCOUNT_ID='<>' \
	-e RESOLVE_TOKEN_ENCRYPTION_KEY='<>' \
	-e CLOUDFLARE_ACCOUNT_ID='<>’ \
	-e CONFIDENCE_CLIENT_ID='<>’ \
	-e CONFIDENCE_CLIENT_SECRET='<>’ \
	-e CONFIDENCE_API_HOST='flags.eu.confidence.dev' \
	-e CONFIDENCE_IAM_HOST='iam.eu.confidence.dev' \
	-e CONFIDENCE_RESOLVER_STATE_ETAG_URL=‘<>/v1/state:etag' \
	image-name
```

The RESOLVE_TOKEN_ENCRYPTION_KEY key has to be a valid AES-128 (16 bytes) key, base64 encoded.
This key is used internally in the resolver, and shouldn't be changed once deployed in production.