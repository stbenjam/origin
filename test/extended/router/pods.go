package router

import (
	"context"
	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	"strings"

	exutil "github.com/openshift/origin/test/extended/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = g.Describe("[sig-network][Early][Feature:Router]", func() {
	defer g.GinkgoRecover()

	oc := exutil.NewCLI("router-pod-check")

	g.BeforeEach(func() {
		var err error
		_, err = exutil.WaitForRouterServiceIP(oc)
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	g.Describe("The HAProxy router pods", func() {
		g.It("should be scheduled on different nodes", func() {
			pods, err := oc.KubeFramework().ClientSet.CoreV1().Pods("openshift-ingress").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				e2e.Failf("unable to list pods: %v", err)
			}
			nodeNameMap := map[string]string{}
			for _, pod := range pods.Items {
				if !strings.Contains(pod.Name, "router-default-") {
					continue
				}
				if podName, ok := nodeNameMap[pod.Spec.NodeName]; ok {
					e2e.Failf("ingress pod %s and pod %s are running on the same node: %s", pod.Name, podName, pod.Spec.NodeName)
				} else {
					nodeNameMap[pod.Spec.NodeName] = pod.Name
				}
			}
		})
	})
})
