package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

type PodMetrics struct {
	Namespace          string
	PodName            string
	CPURequest         string
	CPULimit           string
	CPUUsage           string
	CPUUsagePercent    float64
	MemoryRequest      string
	MemoryLimit        string
	MemoryUsage        string
	MemoryUsagePercent float64
}

func collectMetrics(kubeClient *kubernetes.Clientset, metricsClient *versioned.Clientset, namespace string) ([]*PodMetrics, error) {
	ctx := context.Background()

	// Get all pods
	pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Get pod metrics
	podMetricsList, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod metrics: %w", err)
	}

	// Create a map for quick lookup of metrics
	metricsMap := make(map[string]*metricsv1beta1.PodMetrics)
	for i := range podMetricsList.Items {
		pm := &podMetricsList.Items[i]
		key := fmt.Sprintf("%s/%s", pm.Namespace, pm.Name)
		metricsMap[key] = pm
	}

	var results []*PodMetrics

	for _, pod := range pods.Items {
		key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		podMetric := metricsMap[key]

		// Calculate totals for the pod
		cpuRequest := resource.NewQuantity(0, resource.DecimalSI)
		cpuLimit := resource.NewQuantity(0, resource.DecimalSI)
		memoryRequest := resource.NewQuantity(0, resource.BinarySI)
		memoryLimit := resource.NewQuantity(0, resource.BinarySI)
		cpuUsage := resource.NewQuantity(0, resource.DecimalSI)
		memoryUsage := resource.NewQuantity(0, resource.BinarySI)

		for _, container := range pod.Spec.Containers {
			// Requests
			if req, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				cpuRequest.Add(req)
			}
			if req, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				memoryRequest.Add(req)
			}

			// Limits
			if lim, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
				cpuLimit.Add(lim)
			}
			if lim, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
				memoryLimit.Add(lim)
			}
		}

		// Current usage from metrics
		if podMetric != nil {
			for _, container := range podMetric.Containers {
				cpuUsage.Add(container.Usage[corev1.ResourceCPU])
				memoryUsage.Add(container.Usage[corev1.ResourceMemory])
			}
		}

		// Calculate percentages (based on limits if available, otherwise requests)
		cpuPercent := 0.0
		if cpuLimit.Value() > 0 {
			cpuPercent = float64(cpuUsage.MilliValue()) / float64(cpuLimit.MilliValue()) * 100
		} else if cpuRequest.Value() > 0 {
			cpuPercent = float64(cpuUsage.MilliValue()) / float64(cpuRequest.MilliValue()) * 100
		}

		memoryPercent := 0.0
		if memoryLimit.Value() > 0 {
			memoryPercent = float64(memoryUsage.Value()) / float64(memoryLimit.Value()) * 100
		} else if memoryRequest.Value() > 0 {
			memoryPercent = float64(memoryUsage.Value()) / float64(memoryRequest.Value()) * 100
		}

		metric := &PodMetrics{
			Namespace:          pod.Namespace,
			PodName:            pod.Name,
			CPURequest:         formatCPU(cpuRequest),
			CPULimit:           formatCPU(cpuLimit),
			CPUUsage:           formatCPU(cpuUsage),
			CPUUsagePercent:    cpuPercent,
			MemoryRequest:      formatMemory(memoryRequest),
			MemoryLimit:        formatMemory(memoryLimit),
			MemoryUsage:        formatMemory(memoryUsage),
			MemoryUsagePercent: memoryPercent,
		}

		results = append(results, metric)
	}

	return results, nil
}

func formatCPU(q *resource.Quantity) string {
	if q.IsZero() {
		return "-"
	}

	milliCores := q.MilliValue()
	if milliCores < 1000 {
		return fmt.Sprintf("%dm", milliCores)
	}
	return fmt.Sprintf("%.2f", float64(milliCores)/1000)
}

func formatMemory(q *resource.Quantity) string {
	if q.IsZero() {
		return "-"
	}

	bytes := q.Value()

	const (
		Ki = 1024
		Mi = 1024 * Ki
		Gi = 1024 * Mi
	)

	switch {
	case bytes >= Gi:
		return fmt.Sprintf("%.2fGi", float64(bytes)/float64(Gi))
	case bytes >= Mi:
		return fmt.Sprintf("%.2fMi", float64(bytes)/float64(Mi))
	case bytes >= Ki:
		return fmt.Sprintf("%.2fKi", float64(bytes)/float64(Ki))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
