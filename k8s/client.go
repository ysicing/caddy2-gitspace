package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewKubernetesClient 创建 Kubernetes clientset
// 优先使用集群内配置，如果失败则尝试 kubeconfigPath
func NewKubernetesClient(kubeconfigPath string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	// 尝试集群内配置
	config, err = rest.InClusterConfig()
	if err == nil {
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create clientset: %w", err)
		}
		return clientset, nil
	}

	// 集群内配置失败，尝试 kubeconfig
	if kubeconfigPath == "" {
		// 使用默认 kubeconfig 路径
		if home := os.Getenv("HOME"); home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}
	}

	if kubeconfigPath == "" {
		return nil, fmt.Errorf("no kubeconfig path provided and in-cluster config not available")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return clientset, nil
}

// PatchDeploymentAnnotation 更新 Deployment 的注解
// 使用 Strategic Merge Patch 确保只更新指定的注解
func PatchDeploymentAnnotation(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, name string,
	annotations map[string]string,
) error {
	// 构造 patch 数据
	patchData := map[string]any{
		"metadata": map[string]any{
			"annotations": annotations,
		},
	}

	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal patch data: %w", err)
	}

	// 应用 patch
	_, err = client.AppsV1().Deployments(namespace).Patch(
		ctx,
		name,
		types.StrategicMergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch deployment %s/%s: %w", namespace, name, err)
	}

	return nil
}
