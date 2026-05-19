# Production Setup On GCP

Copy this prompt into an agent that has `gcloud` configured for the target project.

```text
You are deploying jot, a self-hosted private static-hosting service, to GCP.
The final running service should be reachable at a custom domain, gated by
Google Workspace OIDC.

Inputs to ask for:
  - GCP_PROJECT
  - REGION, default europe-west1
  - JOT_DOMAIN, e.g. jot.example.com
  - WORKSPACE_DOMAIN, e.g. example.com
  - JOT_IMAGE, default ghcr.io/skorfmann/jot:latest

Steps:
1. Set and verify the project:
     gcloud config set project $GCP_PROJECT
     gcloud auth list

2. Enable APIs:
     gcloud services enable run.googleapis.com storage.googleapis.com \
       iamcredentials.googleapis.com secretmanager.googleapis.com

3. Create storage:
     gcloud storage buckets create gs://jot-$GCP_PROJECT \
       --location=$REGION --uniform-bucket-level-access

4. Create a service account and grant object access:
     gcloud iam service-accounts create jot-server --display-name="jot server"
     gcloud storage buckets add-iam-policy-binding gs://jot-$GCP_PROJECT \
       --member="serviceAccount:jot-server@$GCP_PROJECT.iam.gserviceaccount.com" \
       --role="roles/storage.objectAdmin"

5. Create HMAC credentials and store them in Secret Manager:
     gcloud storage hmac create jot-server@$GCP_PROJECT.iam.gserviceaccount.com
     echo -n "<accessId>" | gcloud secrets create jot-s3-access --data-file=-
     echo -n "<secret>"   | gcloud secrets create jot-s3-secret --data-file=-

6. Create a cookie secret:
     openssl rand -hex 32 | gcloud secrets create jot-cookie-secret --data-file=-

7. Stop for the manual OAuth client step:
     - Console -> APIs & Services -> Credentials
     - Create OAuth client ID, type Web application, name jot-web
     - Authorized redirect URIs:
       - https://$JOT_DOMAIN/_auth/callback
     - Create OAuth client ID, type Desktop app, name jot-cli
     - Store the web client ID, web client secret, and CLI desktop client ID:
       echo -n "<web-client-id>"     | gcloud secrets create jot-oauth-client-id --data-file=-
       echo -n "<web-client-secret>" | gcloud secrets create jot-oauth-client-secret --data-file=-
       echo -n "<cli-client-id>"     | gcloud secrets create jot-oauth-cli-client-id --data-file=-

8. Grant secret access:
     for s in jot-s3-access jot-s3-secret jot-cookie-secret \
              jot-oauth-client-id jot-oauth-client-secret \
              jot-oauth-cli-client-id; do
       gcloud secrets add-iam-policy-binding $s \
         --member="serviceAccount:jot-server@$GCP_PROJECT.iam.gserviceaccount.com" \
         --role="roles/secretmanager.secretAccessor"
     done

9. Deploy Cloud Run:
     gcloud run deploy jot \
       --image=$JOT_IMAGE \
       --region=$REGION \
       --service-account=jot-server@$GCP_PROJECT.iam.gserviceaccount.com \
       --allow-unauthenticated \
       --port=8080 \
       --min-instances=1 \
       --max-instances=10 \
       --set-env-vars="JOT_SERVER_BASE_URL=https://$JOT_DOMAIN,\
JOT_STORAGE_ENDPOINT=https://storage.googleapis.com,\
JOT_STORAGE_BUCKET=jot-$GCP_PROJECT,\
JOT_STORAGE_REGION=auto,\
JOT_AUTH_ISSUER=https://accounts.google.com,\
JOT_AUTH_AUTHORIZE_HD=$WORKSPACE_DOMAIN,\
JOT_AUTH_SESSION_TTL=8h" \
       --set-secrets="JOT_STORAGE_ACCESS_KEY_ID=jot-s3-access:latest,\
JOT_STORAGE_SECRET_ACCESS_KEY=jot-s3-secret:latest,\
JOT_AUTH_AUDIENCE=jot-oauth-client-id:latest,\
JOT_AUTH_CLIENT_ID=jot-oauth-client-id:latest,\
JOT_AUTH_CLI_CLIENT_ID=jot-oauth-cli-client-id:latest,\
JOT_AUTH_CLIENT_SECRET=jot-oauth-client-secret:latest,\
JOT_AUTH_COOKIE_SECRET=jot-cookie-secret:latest"

10. Map the domain:
      gcloud run domain-mappings create --service=jot \
        --domain=$JOT_DOMAIN --region=$REGION

11. Verify:
      curl -I https://$JOT_DOMAIN/_health

12. Print for the user:
      jot login --server https://$JOT_DOMAIN
      echo '<h1>hello</h1>' > index.html
      jot push index.html --title "First push"
```
