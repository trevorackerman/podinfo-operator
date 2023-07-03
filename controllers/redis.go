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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *MyAppResourceReconciler) reconcileRedisResources(ctx context.Context, myAppResource *myv1alpha1.MyAppResource, log logr.Logger) error {
	redisName := fmt.Sprintf("%s-redis", myAppResource.Name)

	if myAppResource.Spec.Redis.Enabled {
		foundConfig := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundConfig)
		if err != nil && apierrors.IsNotFound(err) {
			redisConfig, err := defineRedisConfigMap(redisName, myAppResource, r.Scheme, log)

			if err != nil {
				return err
			}

			log.Info("Creating Redis ConfigMap", "ConfigMap.Name", redisConfig.Name)
			if err = r.Create(ctx, redisConfig); err != nil {
				log.Error(err, "Failed to create new Redis ConfigMap", "ConfigMap.Name", redisConfig.Name)
				return err
			}
		} else if err != nil {
			log.Error(err, "Failed to get ConfigMap")
			// Let's return the error for the reconciliation be re-trigged again
			return err
		}

		found := &appsv1.Deployment{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, found)

		if err != nil && apierrors.IsNotFound(err) {
			// Define a new deployment
			dep, err := r.defineRedisDeployment(redisName, myAppResource)
			if err != nil {
				log.Error(err, "Failed to define new Redis Deployment resource for MyAppResource")

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

			log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			if err = r.Create(ctx, dep); err != nil {
				log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
				return err
			}

		} else if err != nil {
			log.Error(err, "Failed to get Deployment")
			// Let's return the error for the reconciliation be re-trigged again
			return err
		}

		foundService := &corev1.Service{}

		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundService)
		if err != nil && apierrors.IsNotFound(err) {
			redisService, err := defineRedisService(redisName, myAppResource, r.Scheme, log)

			if err != nil {
				return err
			}

			log.Info("Creating Redis Service", "Service.Name", redisService.Name)
			if err = r.Create(ctx, redisService); err != nil {
				log.Error(err, "Failed to create new Redis Service", "Service.Name", redisService.Name)
				return err
			}
		} else if err != nil {
			log.Error(err, "Failed to get Service")
			// Let's return the error for the reconciliation be re-trigged again
			return err
		}
	} else {
		foundService := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundService)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		} else if err == nil {
			r.Delete(ctx, foundService)
		}

		foundDeployment := &appsv1.Deployment{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundDeployment)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		} else if err == nil {
			r.Delete(ctx, foundDeployment)
		}

		foundConfig := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: redisName, Namespace: myAppResource.Namespace}, foundConfig)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		} else if err == nil {
			r.Delete(ctx, foundConfig)
		}
	}

	return nil
}

func (r *MyAppResourceReconciler) defineRedisDeployment(redisName string, myAppResource *myv1alpha1.MyAppResource) (*appsv1.Deployment, error) {
	ls := labelsForMyAppResource(myAppResource.Name)
	ls["app"] = redisName

	// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/deploy/bases/cache/deployment.yaml
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      redisName,
			Namespace: myAppResource.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
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
					Containers: []corev1.Container{{
						Image:           "redis:7.0.7",
						Name:            "redis",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"redis-server", "/redis-master/redis.conf"},
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
								ContainerPort: 6379,
								Name:          "redis",
							},
						},
						LivenessProbe: &corev1.Probe{
							InitialDelaySeconds: 5,
							TimeoutSeconds:      5,
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.IntOrString{
										IntVal: 6379,
									},
								},
							},
						},
						ReadinessProbe: &corev1.Probe{
							InitialDelaySeconds: 5,
							TimeoutSeconds:      5,
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{"redis-cli", "ping"},
								},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1000m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: "/var/lib/redis",
								Name:      "data",
							},
							{
								MountPath: "/redis-master",
								Name:      "config",
							},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: redisName,
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "redis.conf",
											Path: "redis.conf",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Set the ownerRef for the Deployment
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(myAppResource, dep, r.Scheme); err != nil {
		return nil, err
	}

	return dep, nil
}

func defineRedisConfigMap(redisName string, myAppResource *myv1alpha1.MyAppResource, scheme *runtime.Scheme, log logr.Logger) (*corev1.ConfigMap, error) {
	// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/redis/config.yaml
	redisConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      redisName,
			Namespace: myAppResource.Namespace,
		},
		Data: map[string]string{
			"redis.conf": `maxmemory 64mb
maxmemory-policy allkeys-lru
save ""
appendonly no`,
		},
	}

	if err := ctrl.SetControllerReference(myAppResource, redisConfig, scheme); err != nil {
		log.Error(err, "Failed to set owner reference Redis ConfigMap", "ConfigMap.Name", redisConfig.Name)
		return nil, err
	}

	return redisConfig, nil
}

func defineRedisService(redisName string, myAppResource *myv1alpha1.MyAppResource, scheme *runtime.Scheme, log logr.Logger) (*corev1.Service, error) {
	labels := labelsForMyAppResource(myAppResource.Name)
	labels["app"] = redisName

	// Shamelessly copied from https://github.com/stefanprodan/podinfo/blob/master/charts/podinfo/templates/redis/service.yaml
	redisService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      redisName,
			Namespace: myAppResource.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "redis",
					Port:       6379,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.IntOrString{IntVal: 6379},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(myAppResource, redisService, scheme); err != nil {
		log.Error(err, "Failed to set owner reference Redis Service", "Service.Name", redisService.Name)
		return nil, err

	}
	return redisService, nil
}
