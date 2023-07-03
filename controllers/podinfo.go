package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	myv1alpha1 "github.com/trevorackerman/podinfo-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func podinfoDeploymentsEqual(a *appsv1.Deployment, b *appsv1.Deployment, log logr.Logger) bool {
	if *a.Spec.Replicas != *b.Spec.Replicas {
		log.Info("mismatched replicas", "a", a.Spec.Replicas, "b", b.Spec.Replicas)
		return false
	}

	// Leveraging the fact we only create a single container in the pod
	aContainer := a.Spec.Template.Spec.Containers[0]
	bContainer := b.Spec.Template.Spec.Containers[0]

	if aContainer.Image != bContainer.Image {
		log.Info("Mismatched images", "a", aContainer.Image, "b", bContainer.Image)
		return false
	}

	return envsEqual(aContainer.Env, bContainer.Env, log) &&
		resourcesEqual(aContainer.Resources, bContainer.Resources, log) &&
		commandsEqual(aContainer.Command, bContainer.Command, log)
}

func (r *MyAppResourceReconciler) reconcilePodinfoResources(ctx context.Context, myAppResource *myv1alpha1.MyAppResource, req ctrl.Request, log logr.Logger) error {
	// Check if the deployment already exists, if not create a new one
	podinfoName := fmt.Sprintf("%s-podinfo", myAppResource.Name)
	found := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: podinfoName, Namespace: myAppResource.Namespace}, found)

	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		dep, err := r.definePodinfoDeployment(podinfoName, myAppResource, log)
		if err != nil {
			return r.HandleDeploymentDefinitionError(ctx, myAppResource, err, log)
		}

		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		if err = r.Create(ctx, dep); err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return err
		}

	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		// Let's return the error for the reconciliation be re-trigged again
		return err
	} else {
		log.Info("Checking if found deployment matches desired settings")
		// We've found the deployment, does it need to be updated?
		desired, err := r.definePodinfoDeployment(podinfoName, myAppResource, log)
		if err != nil {
			return r.HandleDeploymentDefinitionError(ctx, myAppResource, err, log)
		}

		if !podinfoDeploymentsEqual(found, desired, log) {
			log.Info("Current podinfo deployment does not match desired state")
			if err = r.Update(ctx, desired); err != nil {
				log.Error(err, "Failed updating Podinfo Deployment", "Namespace", found.Namespace, "Name", found.Name)
				if err := r.Get(ctx, req.NamespacedName, myAppResource); err != nil {
					log.Error(err, "Failed to re-fetch MyAppResource")
					return err
				}

				// The following implementation will update the status
				meta.SetStatusCondition(&myAppResource.Status.Conditions, metav1.Condition{Type: typeAvailableMyAppResource,
					Status: metav1.ConditionFalse, Reason: "Resizing",
					Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", myAppResource.Name, err)})

				if err := r.Status().Update(ctx, myAppResource); err != nil {
					log.Error(err, "Failed to update MyAppResource status")
					return err
				}

				return err
			}
		}
	}

	return nil
}

// definePodinfoDeployment returns a MyAppResource Deployment object
func (r *MyAppResourceReconciler) definePodinfoDeployment(podinfoName string, myAppResource *myv1alpha1.MyAppResource, log logr.Logger) (*appsv1.Deployment, error) {
	ls := labelsForMyAppResource(podinfoName)
	replicas := myAppResource.Spec.ReplicaCount

	// Get the Operand image
	image, err := imageForMyAppResource(myAppResource)
	if err != nil {
		return nil, err
	}

	command := []string{"./podinfo", "--port=9898", "--port-metrics=9797", "--grpc-port=9999", "--grpc-service-name=podinfo", "--level=info", "--random-delay=false", "--random-error=false"}

	if myAppResource.Spec.Redis.Enabled {
		command = append(command, fmt.Sprintf("--cache-server=tcp://%s-redis:6379", myAppResource.Name))
	}

	log.Info("Received resources", "resources", myAppResource.Spec.Resources)
	var cpuRequest resource.Quantity
	if myAppResource.Spec.Resources.CpuRequest.String() == "0" {
		cpuRequest = resource.MustParse("100m")
	} else {
		cpuRequest = myAppResource.Spec.Resources.CpuRequest
	}

	var memoryLimit resource.Quantity
	if myAppResource.Spec.Resources.MemoryLimit.String() == "0" {
		memoryLimit = resource.MustParse("64Mi")
	} else {
		memoryLimit = myAppResource.Spec.Resources.MemoryLimit
	}

	envVars := []corev1.EnvVar{}

	if myAppResource.Spec.UI.Color != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "PODINFO_UI_COLOR", Value: myAppResource.Spec.UI.Color})
	}

	if myAppResource.Spec.UI.Message != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "PODINFO_UI_MESSAGE", Value: myAppResource.Spec.UI.Message})
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podinfoName,
			Namespace: myAppResource.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/kustomize/deployment.yaml
					Containers: []corev1.Container{{
						Image:           image,
						Name:            "podinfo",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{1001}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 9898,
								Name:          "http",
							},
							{
								ContainerPort: 9797,
								Name:          "http-metrics",
							},
							{
								ContainerPort: 9999,
								Name:          "grpc",
							},
						},
						Command: command,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: cpuRequest,
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: memoryLimit,
							},
						},
						Env: envVars,
					}},
				},
			},
		},
	}

	log.Info("deployment container resources", "resources", dep.Spec.Template.Spec.Containers[0].Resources)
	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(myAppResource, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

func imageForMyAppResource(myAppResource *myv1alpha1.MyAppResource) (string, error) {
	return fmt.Sprintf("%s:%s", myAppResource.Spec.Image.Repository, myAppResource.Spec.Image.Tag), nil
}
