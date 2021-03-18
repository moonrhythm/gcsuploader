# gcsuploader

## Deployment

```sh
gcloud run deploy gcsuploader \
  --async \
  --platform=managed \
  --region=asia-northeast1 \
  --concurrency=80 \
  --allow-unauthenticated \
  --timeout=300 \
  --memory=128Mi \
  --image=gcr.io/moonrhythm-containers/gcsuploader \
  --set-env-vars=BUCKET=BUCKET_NAME,BASE_URL=https://example.com \
  --service-account=SERVICE_ACCOUNT
```
