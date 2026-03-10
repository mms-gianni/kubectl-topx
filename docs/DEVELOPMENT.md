## Architecture

The project uses:

- **cobra**: Command-line interface framework
- **tview**: Terminal UI Framework
- **client-go**: Kubernetes Go Client
- **metrics-client**: Kubernetes Metrics API Client

## Development

```bash
# Update modules
go mod tidy

# Run tests (if available)
go test ./...

# Format code
make fmt

# With Make
make help  # Shows all available targets
```

### Build

```bash
# Install dependencies
go mod download

# Build
go build -o kubectl-topx

# Or with Make
make build
```

## Troubleshooting

### "failed to get pod metrics"

This means that the Metrics Server is not installed or not available in your cluster.

**Solution:**
```bash
# Install Metrics Server
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Check if Metrics Server is running
kubectl get deployment metrics-server -n kube-system

# Test
kubectl top nodes
kubectl top pods
```

### "failed to load kubeconfig"

Make sure that your `~/.kube/config` file exists and is valid.

**Solution:**
```bash
# Check kubeconfig
kubectl config view

# Check context
kubectl config current-context

# Test connection
kubectl get nodes
```

### No Pods are Displayed

- Check if pods exist in the selected namespace
- Verify that you have the required RBAC permissions
- Try a different namespace with `--namespace kube-system` or `-n kube-system`

