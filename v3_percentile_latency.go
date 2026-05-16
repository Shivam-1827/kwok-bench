//go:build percentilelatency

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	// 1. Setup Kube Client
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err)
	}

	config.QPS = 100
	config.Burst = 200

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	podCount := 1000
	namespace := "default"
	concurrencyLimit := 50

	fmt.Printf("Starting benchmark: Creating %d pods with concurrency %d...\n", podCount, concurrencyLimit)
	startTime := time.Now()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrencyLimit)

	// 2. Fire 1000 Pods Concurrently
	for i := 0; i < podCount; i++ {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(podIdx int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("kwok-bench-pod-%d", podIdx),
					Labels: map[string]string{
						"app": "kwok-bench",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "nginx", Image: "nginx:latest"},
					},
					NodeSelector: map[string]string{
						"type": "kwok",
					},
					Tolerations: []v1.Toleration{
						{Key: "kwok.x-k8s.io/node", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
					},
				},
			}

			_, err := clientset.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
			if err != nil {
				fmt.Printf("Error creating pod %d: %s\n", podIdx, err.Error())
			}
		}(i)
	}

	wg.Wait()
	createTime := time.Since(startTime)
	fmt.Printf("Finished API creation calls in %v.\n", createTime)
	fmt.Println("Waiting for all pods to be Scheduled...")

	var finalPods []v1.Pod
	for {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=kwok-bench",
		})
		if err != nil {
			panic(err)
		}

		scheduledCount := 0
		for _, p := range pods.Items {
			for _, cond := range p.Status.Conditions {
				if cond.Type == v1.PodScheduled && cond.Status == v1.ConditionTrue {
					scheduledCount++
					break
				}
			}
		}

		if scheduledCount >= podCount {
			finalPods = pods.Items 
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	totalTime := time.Since(startTime)
	throughput := float64(podCount) / totalTime.Seconds()

	//  P50, P90, P99 LATENCY CALCULATION 
	var latencies []time.Duration
	for _, p := range finalPods {
		creationTime := p.CreationTimestamp.Time
		var scheduledTime time.Time

		for _, cond := range p.Status.Conditions {
			if cond.Type == v1.PodScheduled && cond.Status == v1.ConditionTrue {
				scheduledTime = cond.LastTransitionTime.Time
				break
			}
		}

		if !scheduledTime.IsZero() {
			latency := scheduledTime.Sub(creationTime)
			latencies = append(latencies, latency)
		}
	}

	var p50, p90, p99 time.Duration
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})

		p50 = latencies[int(float64(len(latencies))*0.50)]
		p90 = latencies[int(float64(len(latencies))*0.90)]
		p99 = latencies[int(float64(len(latencies))*0.99)]
	}

	fmt.Printf("\nSUCCESS: 1000 pods successfully scheduled!\n")
	fmt.Printf("Total Time: %.2f seconds\n", totalTime.Seconds())
	fmt.Printf("Throughput: %.2f pods/sec\n\n", throughput)

	fmt.Println("--- Scheduling Latency Percentiles ---")
	fmt.Printf("P50 (Median): %v\n", p50)
	fmt.Printf("P90:          %v\n", p90)
	fmt.Printf("P99 (Tail):   %v\n", p99)
}
