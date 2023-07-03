package controllers

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	myv1alpha1 "github.com/trevorackerman/podinfo-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MyAppResource controller", func() {
	Context("MyAppResource controller test", func() {
		const MyAppResourceName = "myappresource-sample"

		ctx := context.Background()

		typeNamespaceName := types.NamespacedName{Name: MyAppResourceName, Namespace: "default"}

		// TODO - negative testing of replica count
		// TODO - test of scaling replica count

		It("Successfully Reconciles", func() {
			myAppResource := &myv1alpha1.MyAppResource{}
			err := k8sClient.Get(ctx, typeNamespaceName, myAppResource)

			if err != nil {
				Expect(errors.IsNotFound(err)).To(BeTrue())

				myAppResource = createMyAppResource(ctx, "../config/samples/my_v1alpha1_myappresource.yaml", myAppResource)
			}

			By("Checking if the resource was created")
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespaceName, &myv1alpha1.MyAppResource{})
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the resource")
			myAppResourceReconciler := &MyAppResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = myAppResourceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking the Deployment was successfully created")
			Eventually(func() error {
				return checkPodinfoDeployment(ctx, myAppResource)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the ConfigMap was successfully created")
			Eventually(func() error {
				return checkRedisConfig(ctx, myAppResource)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the Redis Deployment was created")
			Eventually(func() error {
				return checkRedisDeployment(ctx, myAppResource)
			}, time.Minute, time.Second).Should(Succeed())

			Eventually(func() error {
				return checkRedisService(ctx, myAppResource)
			}, time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition")
			Eventually(func() error {
				if myAppResource.Status.Conditions != nil && len(myAppResource.Status.Conditions) > 0 {
					latestStatusCondition := myAppResource.Status.Conditions[len(myAppResource.Status.Conditions)-1]
					expected := metav1.Condition{
						Type:    typeAvailableMyAppResource,
						Status:  metav1.ConditionTrue,
						Reason:  "Reconciling",
						Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", myAppResource.Name, myAppResource.Spec.ReplicaCount),
					}

					if latestStatusCondition != expected {
						return fmt.Errorf("The latest status condition added to the myAppResource instance is not as expected")
					}
				}

				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})

		It("updates resources", func() {
			myAppResource := &myv1alpha1.MyAppResource{}
			err := k8sClient.Get(ctx, typeNamespaceName, myAppResource)
			Expect(err).To(BeNil())

			myAppResource.Spec.UI.Message = "Goodnight Moon"
			myAppResource.Spec.UI.Color = "#332211"
			err = k8sClient.Update(ctx, myAppResource)
			Expect(err).To(BeNil())

			By("Reconciling the resource")
			myAppResourceReconciler := &MyAppResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = myAppResourceReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			Eventually(func() []v1.EnvVar {
				deployment := &appsv1.Deployment{}
				err = k8sClient.Get(ctx, podinfoNamespacedName(myAppResource), deployment)
				Expect(err).To(BeNil())

				fmt.Println("env", deployment.Spec.Template.Spec.Containers[0].Env)
				return deployment.Spec.Template.Spec.Containers[0].Env
			}, time.Minute, time.Second).Should(Equal(
				[]v1.EnvVar{
					{Name: "PODINFO_UI_COLOR", Value: "#332211"},
					{Name: "PODINFO_UI_MESSAGE", Value: "Goodnight Moon"},
				},
			))
		})
	})
})

func createMyAppResource(ctx context.Context, yamlPath string, myAppResource *myv1alpha1.MyAppResource) *myv1alpha1.MyAppResource {
	yamlBytes, err := os.ReadFile(yamlPath)
	Expect(err).To(BeNil())

	By("Creating the custom resource for the Kind MyAppResource")
	err = yaml.Unmarshal(yamlBytes, &myAppResource)
	Expect(err).To(BeNil())

	myAppResource.ObjectMeta.Namespace = "default"

	err = k8sClient.Create(ctx, myAppResource)
	Expect(err).To(Not(HaveOccurred()))

	return myAppResource
}

func checkPodinfoDeployment(ctx context.Context, resource *myv1alpha1.MyAppResource) error {
	found := &appsv1.Deployment{}
	resourceName := fmt.Sprintf("%s-podinfo", resource.Name)
	err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, found)
	if err != nil {
		return err
	}

	Expect(*found.Spec.Replicas).To(Equal(int32(2)))

	checkOwnerReference(found.ObjectMeta.OwnerReferences[0], resource)

	container := found.Spec.Template.Spec.Containers[0]
	Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
	Expect(container.Resources.Limits.Memory().String()).To(Equal("64Mi"))
	Expect(container.Image).To(Equal("ghcr.io/stefanprodan/podinfo:6.3.6"))
	Expect(container.Env).To(Equal([]v1.EnvVar{
		{Name: "PODINFO_UI_COLOR", Value: "#CC8844"},
		{Name: "PODINFO_UI_MESSAGE", Value: "Howdy!"},
	}))
	Expect(container.Command).To(Equal([]string{
		"./podinfo",
		"--port=9898",
		"--port-metrics=9797",
		"--grpc-port=9999",
		"--grpc-service-name=podinfo",
		"--level=info",
		"--random-delay=false",
		"--random-error=false",
		fmt.Sprintf("--cache-server=tcp://%s:6379", redisName(resource)),
	}))
	return nil
}

func podinfoName(resource *myv1alpha1.MyAppResource) string {
	return fmt.Sprintf("%s-podinfo", resource.Name)
}

func podinfoNamespacedName(resource *myv1alpha1.MyAppResource) types.NamespacedName {
	return types.NamespacedName{Namespace: resource.Namespace, Name: podinfoName(resource)}
}

func checkRedisConfig(ctx context.Context, resource *myv1alpha1.MyAppResource) error {
	foundRedisConfig := &v1.ConfigMap{}
	err := k8sClient.Get(ctx, redisNamespacedName(resource), foundRedisConfig)
	if err != nil {
		return err
	}

	Expect(foundRedisConfig.Data).To(Equal(map[string]string{
		"redis.conf": `maxmemory 64mb
maxmemory-policy allkeys-lru
save ""
appendonly no`,
	}))

	Expect(len(foundRedisConfig.OwnerReferences)).To(Equal(1))
	checkOwnerReference(foundRedisConfig.ObjectMeta.OwnerReferences[0], resource)
	return nil
}

func checkRedisDeployment(ctx context.Context, resource *myv1alpha1.MyAppResource) error {
	foundRedis := &appsv1.Deployment{}
	err := k8sClient.Get(ctx, redisNamespacedName(resource), foundRedis)
	if err != nil {
		return err
	}

	checkOwnerReference(foundRedis.OwnerReferences[0], resource)

	redisContainer := foundRedis.Spec.Template.Spec.Containers[0]
	Expect(redisContainer.Image).To(Equal("redis:7.0.7"))

	return nil
}

func checkRedisService(ctx context.Context, resource *myv1alpha1.MyAppResource) error {
	foundRedisService := &v1.Service{}
	err := k8sClient.Get(ctx, redisNamespacedName(resource), foundRedisService)
	if err != nil {
		return err
	}

	Expect(foundRedisService.Spec.Type).To(Equal(v1.ServiceTypeClusterIP))
	Expect(foundRedisService.Spec.Ports[0].Port).To(Equal(int32(6379)))
	checkOwnerReference(foundRedisService.OwnerReferences[0], resource)
	return nil
}

func redisName(resource *myv1alpha1.MyAppResource) string {
	return fmt.Sprintf("%s-redis", resource.Name)
}

func redisNamespacedName(resource *myv1alpha1.MyAppResource) types.NamespacedName {
	return types.NamespacedName{Namespace: resource.Namespace, Name: redisName(resource)}
}

func checkOwnerReference(ownerReference metav1.OwnerReference, resource *myv1alpha1.MyAppResource) {
	Expect(ownerReference.APIVersion).To(Equal("my.api.group/v1alpha1"))
	Expect(ownerReference.Kind).To(Equal("MyAppResource"))
	Expect(ownerReference.Name).To(Equal(resource.Name))
}
