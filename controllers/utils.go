package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	myv1alpha1 "github.com/trevorackerman/podinfo-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// labelsForMyAppResource returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForMyAppResource(name string) map[string]string {
	return map[string]string{"app.kubernetes.io/name": "MyAppResource",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/part-of":    "podinfo-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

func envsEqual(a []corev1.EnvVar, b []corev1.EnvVar, log logr.Logger) bool {
	if len(a) != len(b) {
		log.Info("different length envs", "a", len(a), "b", len(b))
		return false
	}

	m := make(map[string]string)

	for _, item := range a {
		m[item.Name] = item.Value
	}

	for _, item := range b {
		val, ok := m[item.Name]
		if ok && val == item.Value {
			delete(m, item.Name)
		} else if !ok {
			m[item.Name] = item.Value
		}
	}

	if len(m) != 0 {
		for key, val := range m {
			log.Info("Found mismatched env var", "key", key, "value", val)
		}
		return false
	}
	return true
}

func resourcesEqual(a corev1.ResourceRequirements, b corev1.ResourceRequirements, log logr.Logger) bool {
	if !a.Limits.Memory().Equal(*b.Limits.Memory()) {
		log.Info("Mismatched memory limits", "a", a.Limits.Memory(), "b", b.Limits.Memory())
		return false
	}

	if !a.Requests.Cpu().Equal(*b.Requests.Cpu()) {
		log.Info("Mismatched CPU requests", "a", a.Requests.Cpu(), "b", b.Requests.Cpu())
		return false
	}

	return true
}

func commandsEqual(a []string, b []string, log logr.Logger) bool {
	if len(a) != len(b) {
		log.Info("Mismatched commands (len)", "a", len(a), "b", len(b))
		return false
	}

	ok := true
	for i, item := range a {
		if b[i] != item {
			ok = false
			log.Info("Found mismatched string in command", "index", i, "a", a[i], "b", b[i])
		}
	}

	return ok
}

func (r *MyAppResourceReconciler) HandleDeploymentDefinitionError(ctx context.Context, myAppResource *myv1alpha1.MyAppResource, err error, log logr.Logger) error {
	log.Error(err, "Failed to define new Deployment resource for MyAppResource")

	// The following implementation will update the status
	meta.SetStatusCondition(
		&myAppResource.Status.Conditions,
		metav1.Condition{
			Type:    typeAvailableMyAppResource,
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: fmt.Sprintf("Failed to create Deployment for the custom resource (%s): (%s)", myAppResource.Name, err),
		},
	)

	if err := r.Status().Update(ctx, myAppResource); err != nil {
		log.Error(err, "Failed to update MyAppResource status")
		return err
	}

	return err
}
