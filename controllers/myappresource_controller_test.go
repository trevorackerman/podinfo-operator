package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	myv1alpha1 "github.com/trevorackerman/podinfo-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MyAppResource controller", func() {
	Context("MyAppResource controller test", func() {
		const MyAppResourceName = "test-resource"

		ctx := context.Background()

		typeNamespaceName := types.NamespacedName{Name: MyAppResourceName, Namespace: "default"}

		// TODO - negative testing of replica count
		// TODO - test of scaling replica count

		It("Successfully Reconciles", func() {
			By("Creating the custom resource for the Kind MyAppResource")
			myAppResource := &myv1alpha1.MyAppResource{}
			err := k8sClient.Get(ctx, typeNamespaceName, myAppResource)

			if err != nil {
				Expect(errors.IsNotFound(err)).To(BeTrue())
				myAppResource := &myv1alpha1.MyAppResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MyAppResourceName,
						Namespace: "default",
					},
					Spec: myv1alpha1.MyAppResourceSpec{
						ReplicaCount: 3,
						Resources: myv1alpha1.Resources{
							MemoryLimit: resource.MustParse("64Mi"),
							CpuRequest:  resource.MustParse("100m"),
						},
						Image: myv1alpha1.Image{
							Repository: "nginx",
							Tag:        "1.14.2",
						},
						UI: myv1alpha1.UI{
							Color:   "#dedbed",
							Message: "Hello World!",
						},
						Redis: myv1alpha1.Redis{
							Enabled: true,
						},
					},
				}

				err = k8sClient.Create(ctx, myAppResource)
				Expect(err).To(Not(HaveOccurred()))
			}

			By("Checking if the resource was created")
			Eventually(func() error {
				found := &myv1alpha1.MyAppResource{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
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
				found := &appsv1.Deployment{}
				podinfoName := fmt.Sprintf("%s-podinfo", MyAppResourceName)
				err := k8sClient.Get(ctx, types.NamespacedName{Name: podinfoName, Namespace: "default"}, found)
				if err != nil {
					return err
				}

				Expect(*found.Spec.Replicas).To(Equal(int32(3)))
				ownerReference := found.ObjectMeta.OwnerReferences[0]
				Expect(ownerReference.APIVersion).To(Equal("my.api.group/v1alpha1"))
				Expect(ownerReference.Kind).To(Equal("MyAppResource"))
				Expect(ownerReference.Name).To(Equal(MyAppResourceName))

				container := found.Spec.Template.Spec.Containers[0]
				Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
				Expect(container.Resources.Limits.Memory().String()).To(Equal("64Mi"))
				Expect(container.Image).To(Equal("nginx:1.14.2"))
				Expect(container.Env).To(Equal([]v1.EnvVar{
					{Name: "PODINFO_UI_COLOR", Value: "#dedbed"},
					{Name: "PODINFO_UI_MESSAGE", Value: "Hello World!"},
				}))

				foundRedisConfig := &v1.ConfigMap{}
				err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-redis", MyAppResourceName)}, foundRedisConfig)
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
				ownerReference = foundRedisConfig.ObjectMeta.OwnerReferences[0]
				Expect(ownerReference.APIVersion).To(Equal("my.api.group/v1alpha1"))
				Expect(ownerReference.Kind).To(Equal("MyAppResource"))
				Expect(ownerReference.Name).To(Equal(MyAppResourceName))

				foundRedis := &appsv1.Deployment{}
				err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-redis", MyAppResourceName)}, foundRedis)
				if err != nil {
					return err
				}

				ownerReference = foundRedis.ObjectMeta.OwnerReferences[0]
				Expect(ownerReference.APIVersion).To(Equal("my.api.group/v1alpha1"))
				Expect(ownerReference.Kind).To(Equal("MyAppResource"))
				Expect(ownerReference.Name).To(Equal(MyAppResourceName))

				redisContainer := foundRedis.Spec.Template.Spec.Containers[0]
				Expect(redisContainer.Image).To(Equal("redis:7.0.7"))

				foundRedisService := &v1.Service{}
				err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("%s-redis", MyAppResourceName)}, foundRedisService)
				if err != nil {
					return err
				}

				Expect(foundRedisService.Spec.Type).To(Equal(v1.ServiceTypeClusterIP))
				Expect(foundRedisService.Spec.Ports[0].Port).To(Equal(int32(6379)))
				return nil
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
	})
})
