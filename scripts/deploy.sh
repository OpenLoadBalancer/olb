#!/bin/bash
#
# OpenLoadBalancer Production Deployment Script
# Usage: ./deploy.sh [environment]
# Environments: staging, production
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
ENVIRONMENT=${1:-staging}
VERSION=${2:-latest}
DOCKER_IMAGE="openloadbalancer/olb:${VERSION}"

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Pre-deployment checks
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check Docker
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed"
        exit 1
    fi

    # Check docker-compose
    if ! command -v docker-compose &> /dev/null; then
        log_error "docker-compose is not installed"
        exit 1
    fi

    # Check if config exists
    if [ ! -f "configs/olb.yaml" ]; then
        log_warn "Config file not found at configs/olb.yaml"
        log_info "Creating from example..."
        cp configs/olb.yaml.example configs/olb.yaml 2>/dev/null || true
    fi

    log_success "Prerequisites check passed"
}

# Build application
build_application() {
    log_info "Building OpenLoadBalancer..."

    # Build frontend
    log_info "Building frontend..."
    cd internal/webui
    npm ci
    npm run build
    cd ../..

    # Build Go binary
    log_info "Building Go binary..."
    go build -o bin/olb ./cmd/olb/

    log_success "Build completed"
}

# Run tests
run_tests() {
    log_info "Running tests..."

    # Run Go tests
    go test -race -count=1 ./... -timeout=600s

    log_success "All tests passed"
}

# Build Docker image
build_docker() {
    log_info "Building Docker image..."

    docker build -t ${DOCKER_IMAGE} .

    log_success "Docker image built: ${DOCKER_IMAGE}"
}

# Deploy to environment
deploy() {
    log_info "Deploying to ${ENVIRONMENT}..."

    case ${ENVIRONMENT} in
        staging)
            deploy_staging
            ;;
        production)
            deploy_production
            ;;
        *)
            log_error "Unknown environment: ${ENVIRONMENT}"
            log_info "Usage: ./deploy.sh [staging|production]"
            exit 1
            ;;
    esac
}

# Staging deployment
deploy_staging() {
    log_info "Deploying to staging..."

    # Stop existing containers
    docker-compose -f docker-compose.staging.yml down --remove-orphans 2>/dev/null || true

    # Start new deployment
    docker-compose -f docker-compose.staging.yml up -d

    # Wait for health check
    sleep 5

    # Check health
    if curl -s http://localhost:8080/health > /dev/null; then
        log_success "Staging deployment successful"
    else
        log_error "Staging health check failed"
        docker-compose -f docker-compose.staging.yml logs
        exit 1
    fi
}

# Production deployment with blue-green strategy
deploy_production() {
    log_info "Deploying to production..."

    log_warn "Starting production deployment with blue-green strategy"

    # Determine current color
    CURRENT_COLOR=$(docker-compose ps | grep -oE "(blue|green)" | head -1 || echo "blue")

    if [ "$CURRENT_COLOR" = "blue" ]; then
        NEW_COLOR="green"
    else
        NEW_COLOR="blue"
    fi

    log_info "Current: ${CURRENT_COLOR}, Deploying: ${NEW_COLOR}"

    # Deploy new color
    docker-compose -f docker-compose.${NEW_COLOR}.yml up -d

    # Wait for health check
    log_info "Waiting for health check..."
    sleep 10

    NEW_PORT=$(docker-compose -f docker-compose.${NEW_COLOR}.yml port olb 8080 | cut -d: -f2)

    if curl -s http://localhost:${NEW_PORT}/health > /dev/null; then
        log_success "New deployment is healthy"
    else
        log_error "New deployment health check failed"
        docker-compose -f docker-compose.${NEW_COLOR}.yml down
        exit 1
    fi

    # Switch traffic (requires external load balancer configuration)
    log_info "Switching traffic to ${NEW_COLOR}..."

    # Update load balancer config (example with nginx)
    # sed -i "s/proxy_pass http:\/\/localhost:[0-9]*/proxy_pass http:\/\/localhost:${NEW_PORT}/" /etc/nginx/conf.d/olb.conf
    # nginx -s reload

    # Stop old color after grace period
    log_info "Waiting grace period (30s)..."
    sleep 30

    docker-compose -f docker-compose.${CURRENT_COLOR}.yml down

    log_success "Production deployment completed. Active: ${NEW_COLOR}"
}

# Rollback function
rollback() {
    log_warn "Initiating rollback..."

    case ${ENVIRONMENT} in
        staging)
            docker-compose -f docker-compose.staging.yml down
            docker-compose -f docker-compose.staging.yml up -d
            ;;
        production)
            # Switch back to previous color
            if [ "$NEW_COLOR" = "blue" ]; then
                docker-compose -f docker-compose.green.yml up -d
                docker-compose -f docker-compose.blue.yml down
            else
                docker-compose -f docker-compose.blue.yml up -d
                docker-compose -f docker-compose.green.yml down
            fi
            ;;
    esac

    log_success "Rollback completed"
}

# Main execution
main() {
    log_info "OpenLoadBalancer Deployment Script"
    log_info "Environment: ${ENVIRONMENT}"
    log_info "Version: ${VERSION}"
    echo ""

    # Trap errors for rollback
    trap 'log_error "Deployment failed!"; rollback' ERR

    check_prerequisites

    # Ask for confirmation in production
    if [ "${ENVIRONMENT}" = "production" ]; then
        log_warn "You are about to deploy to PRODUCTION"
        read -p "Are you sure? (yes/no): " confirm
        if [ "$confirm" != "yes" ]; then
            log_info "Deployment cancelled"
            exit 0
        fi
    fi

    build_application
    run_tests
    build_docker
    deploy

    echo ""
    log_success "Deployment to ${ENVIRONMENT} completed successfully!"

    # Print status
    echo ""
    log_info "Deployment Status:"
    docker ps | grep olb || true
}

# Show help
show_help() {
    cat << EOF
OpenLoadBalancer Deployment Script

Usage: ./deploy.sh [OPTIONS] [ENVIRONMENT]

Arguments:
  ENVIRONMENT    Target environment (staging, production)
                 Default: staging

Options:
  -h, --help     Show this help message
  -v, --version  Set version tag (default: latest)

Examples:
  ./deploy.sh staging              # Deploy to staging
  ./deploy.sh production v1.2.3   # Deploy v1.2.3 to production
  ./deploy.sh --help              # Show help

EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        *)
            ENVIRONMENT="$1"
            shift
            ;;
    esac
done

# Run main
main
