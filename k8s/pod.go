package k8s

import (
	corev1 "k8s.io/api/core/v1"
)

type SimplePod struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Status    *corev1.PodStatus `json:"status,omitempty"`
}

type Pod struct {
	SimplePod
	*corev1.Pod
}

func NewPod(pod *corev1.Pod) *Pod {
	p := Pod{Pod: pod}

	p.SimplePod.Name = p.Pod.Name
	p.SimplePod.Namespace = p.Pod.Namespace
	p.SimplePod.Status = &p.Pod.Status

	return &p
}
