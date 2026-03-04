# Example Output

```
┌─────────────────────────────────────────────────────────────────-──────────┐
│            Kubernetes Resource Metrics Monitor                             │
│        Press 'q' to quit | Press 'r' to refresh now                        │
├──────────────────────────────────────────────────────────────────-─────────┤
│ Namespace │ Pod              │ CPU Req │ CPU Lim │ CPU Usage               │
│           │                  │         │         │                         │
│ default   │ nginx-abc123     │ 100m    │ 200m    │ 45m  [████████░░░] 22.5%│
│ default   │ redis-xyz789     │ 250m    │ 500m    │ 387m [██████████░] 77.4%│
│ kube-sys  │ coredns-1234     │ 100m    │ -       │ 12m  [██░░░░░░░░░] 12.0%│
│ kube-sys  │ kube-proxy-567   │ -       │ -       │ 5m   [░░░░░░░░░░░] 0.0% │
└───────────────────────────────────────────────────────────────────-────────┘
│     Monitoring: all namespaces | Refresh: 5s | Last update: 14:30:45       │
└────────────────────────────────────────────────────────────────────────-───┘
```

## Color Coding

The bars and percentages are color-highlighted:

- **Green** (< 50%): Normal usage
- **Yellow** (50-75%): Elevated usage
- **Orange** (75-90%): High usage
- **Red** (>= 90%): Critical usage

## Percentage Calculation

The percentage values are calculated as follows:

1. **If limits are defined**: `(Usage / Limit) * 100`
2. **If only requests are defined**: `(Usage / Request) * 100`
3. **If nothing is defined**: Displayed as 0%

## Special Features

- Pods without metrics (k8s metrics server not available) show 0%
- `-` means no request/limit is defined
- CPU is displayed in millicores (m) or cores
- Memory is displayed in Ki, Mi, or Gi
