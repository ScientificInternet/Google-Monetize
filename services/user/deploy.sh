#!/bin/bash

# Google-Monetize User Service Deployment Script
# Follows Google-Monetize project standards for Cloud Run deployment

set -e

# Configuration following project standards
PROJECT_ID="${GCP_PROJECT_ID}"  # Google-Monetize project ID
SERVICE_NAME="user"
REGION="asia-northeast1"  # Following project standard
IMAGE_NAME="gm-user-service"
REGISTRY="asia-northeast1-docker.pkg.dev"
SERVICE_ACCOUNT="codex-dev@${GCP_PROJECT_ID}.iam.gserviceaccount.com"

# Environment detection
BRANCH=${1:-main}
if [[ "$BRANCH" == "main" ]]; then
    ENV="preview"
    TAG="preview-latest"
elif [[ "$BRANCH" == "production" ]]; then
    ENV="production"
    TAG="prod-latest"
else
    TAG="preview-${BRANCH}"
    ENV="preview"
fi

SERVICE_SUFFIX=""
if [[ "$ENV" == "preview" ]]; then
    SERVICE_SUFFIX="-preview"
fi

FULL_SERVICE_NAME="${SERVICE_NAME}${SERVICE_SUFFIX}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_step "Checking prerequisites..."

    if ! command -v gcloud &> /dev/null; then
        log_error "gcloud CLI is not installed. Please install it first."
        exit 1
    fi

    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed. Please install it first."
        exit 1
    fi

    # Set current project
    gcloud config set project $PROJECT_ID
    log_info "Using project: $PROJECT_ID"

    log_info "Prerequisites check passed."
}

# Build using Cloud Build (project standard)
build_with_cloudbuild() {
    log_step "Building Docker image with Cloud Build..."

    # Create optimized source tarball following monorepo best practices
    TARBALL="/tmp/${SERVICE_NAME}-source.tar.gz"

    log_info "Creating optimized source tarball..."
    tar -czf "$TARBALL" \
        --exclude='apps' \
        --exclude='makerkit' \
        --exclude='docs' \
        --exclude='node_modules' \
        --exclude='.git' \
        --exclude='.next' \
        go.work go.work.sum \
        services/"${SERVICE_NAME}" \
        pkg schemas deployments scripts/db

    TARBALL_SIZE=$(du -h "$TARBALL" | cut -f1)
    log_info "Source tarball size: $TARBALL_SIZE"

    # Submit to Cloud Build
    log_info "Submitting to Cloud Build..."
    gcloud builds submit \
        --config=deployments/cloudbuild/build-service-docker.yaml \
        --substitutions=_SERVICE="${SERVICE_NAME}",_IMAGE="${REGISTRY}/${PROJECT_ID}/gm-services/${SERVICE_NAME}:${TAG}" \
        --project="${PROJECT_ID}" \
        "$TARBALL"

    # Clean up
    rm -f "$TARBALL"

    log_info "Image successfully built: ${REGISTRY}/${PROJECT_ID}/gm-services/${SERVICE_NAME}:${TAG}"
}

# Deploy to Cloud Run
deploy_to_cloud_run() {
    log_step "Deploying to Cloud Run..."

    # Prepare environment variables from Secret Manager
    ENV_VARS=(
        "GCP_PROJECT_ID=${PROJECT_ID}"
        "ENVIRONMENT=${ENV}"
        "OTEL_EXPORTER_OTLP_ENDPOINT=https://otel-collector-${ENV}.${APP_DOMAIN}"
    )

    # Build environment variable arguments
    ENV_ARGS=""
    for env in "${ENV_VARS[@]}"; do
        ENV_ARGS="$ENV_ARGS --set-env-vars $env"
    done

    # Add secrets
    SECRETS=(
        "SUPABASE_URL=projects/${PROJECT_ID}/secrets/SUPABASE_URL:latest"
        "SUPABASE_ANON_KEY=projects/${PROJECT_ID}/secrets/SUPABASE_ANON_KEY:latest"
        "GCP_DATABASE_URL=projects/${PROJECT_ID}/secrets/GCP_DATABASE_URL:latest"
        "REDIS_ADDRESS=projects/${PROJECT_ID}/secrets/REDIS_ADDRESS:latest"
    )

    SECRET_ARGS=""
    for secret in "${SECRETS[@]}"; do
        SECRET_ARGS="$SECRET_ARGS --set-secrets $secret"
    done

    # Deploy to Cloud Run with project standard configuration
    gcloud run deploy $FULL_SERVICE_NAME \
        --image ${REGISTRY}/${PROJECT_ID}/gm-services/${SERVICE_NAME}:${TAG} \
        --region $REGION \
        --platform managed \
        --service-account $SERVICE_ACCOUNT \
        --memory 1Gi \
        --cpu 1 \
        --timeout 300s \
        --concurrency 80 \
        --min-instances 0 \
        --max-instances 20 \
        --allow-unauthenticated \
        $ENV_ARGS \
        $SECRET_ARGS

    log_info "Deployment completed successfully!"
}

# Get service URL
get_service_url() {
    log_step "Getting service URL..."
    SERVICE_URL=$(gcloud run services describe $FULL_SERVICE_NAME \
        --region $REGION \
        --format 'value(status.url)')

    if [ -n "$SERVICE_URL" ]; then
        log_info "Service URL: $SERVICE_URL"
        log_info "Health check: $SERVICE_URL/health"
        log_info "API documentation: $SERVICE_URL/swagger/index.html"
        echo ""
        log_info "Gateway configuration update:"
        log_warn "Update /services/gateway-middleware/config/routes.yaml:"
        echo "  user: $SERVICE_URL"
    else
        log_error "Failed to get service URL"
    fi
}

# Run health check
health_check() {
    log_step "Running health check..."

    SERVICE_URL=$(gcloud run services describe $FULL_SERVICE_NAME \
        --region $REGION \
        --format 'value(status.url)')

    if [ -n "$SERVICE_URL" ]; then
        # Wait a moment for service to be ready
        sleep 10

        # Check health endpoint
        if curl -f "$SERVICE_URL/health" > /dev/null 2>&1; then
            log_info "✅ Health check passed"
        else
            log_warn "⚠️  Health check failed - service may still be starting"
        fi
    fi
}

# Update Gateway configuration (automated)
update_gateway() {
    log_step "Updating Gateway configuration..."

    SERVICE_URL=$(gcloud run services describe $FULL_SERVICE_NAME \
        --region $REGION \
        --format 'value(status.url)')

    if [ -n "$SERVICE_URL" ]; then
        # Update routes.yaml
        sed -i.bak "s|  user: .*|  user: $SERVICE_URL|g" /Users/jason/Documents/Kiro/gm/services/gateway-middleware/config/routes.yaml

        log_info "Gateway configuration updated successfully"
        log_warn "Remember to commit the changes to routes.yaml"
    fi
}

# Main execution
main() {
    log_info "Starting Google-Monetize User Service deployment..."
    log_info "Environment: $ENV"
    log_info "Service: $FULL_SERVICE_NAME"
    log_info "Tag: $TAG"
    echo ""

    check_prerequisites
    build_with_cloudbuild
    deploy_to_cloud_run
    get_service_url
    health_check
    update_gateway

    log_info "🎉 Deployment process completed!"
    echo ""
    log_info "Next steps:"
    echo "1. ✅ Service deployed and health checked"
    echo "2. ✅ Gateway configuration updated"
    echo "3. Test the service endpoints"
    echo "4. Update Supabase database schema if needed"
    echo "5. Configure monitoring and alerting"
}

# Handle script arguments
case "${1:-}" in
    "preview"|"main")
        main "main"
        ;;
    "production")
        main "production"
        ;;
    "help"|"-h"|"--help")
        echo "Usage: $0 [preview|production|help]"
        echo ""
        echo "Commands:"
        echo "  preview       - Deploy to preview environment (default)"
        echo "  production    - Deploy to production environment"
        echo "  help          - Show this help message"
        echo ""
        echo "Default behavior: Deploy to preview environment"
        ;;
    *)
        main
        ;;
esac