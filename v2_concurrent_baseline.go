//go:build concurrentbaseline

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
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
	fmt.Println("Waiting for all pods to be Scheduled/Running...")

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
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	totalTime := time.Since(startTime)
	throughput := float64(podCount) / totalTime.Seconds()

	fmt.Printf("\nSUCCESS: 1000 pods scheduled and running!\n")
	fmt.Printf("Total Time: %.2f seconds\n", totalTime.Seconds())
	fmt.Printf("Throughput: %.2f pods/sec\n", throughput)
}
