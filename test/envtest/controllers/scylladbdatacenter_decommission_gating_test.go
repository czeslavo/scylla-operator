//go:build envtest

package controllers

import (
	"context"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	oslices "github.com/scylladb/scylla-operator/pkg/helpers/slices"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/test/envtest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = g.Describe("ScyllaDBDatacenter controller decommission gating", func() {
	var env *envtest.Environment
	g.BeforeEach(func(ctx g.SpecContext) {
		env = envtest.Setup(ctx)
	})

	g.It("should commit a scale down to status and defer node count changes until it concludes", func(ctx g.SpecContext) {
		g.By("Running ScyllaDBDatacenter controller")
		runScyllaDBDatacenterController(ctx, env)

		g.By("Running a fake StatefulSet rollout syncer")
		runFakeStatefulSetRolloutSyncer(ctx, env)

		g.By("Creating ScyllaOperatorConfig singleton")
		createScyllaOperatorConfig(ctx, env)

		g.By("Creating a ScyllaDBDatacenter with a three-node rack")
		sdc := makeEnvtestScyllaDBDatacenter(env.Namespace(), []string{"rack-a"}, withEnableParallelNodeOperations(false), withRackTemplateNodes(3))
		sdc, err := env.ScyllaClient().ScyllaV1alpha1().ScyllaDBDatacenters(env.Namespace()).Create(ctx, sdc, metav1.CreateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("Waiting for the rack StatefulSet and member services to be created")
		statefulSetName := naming.StatefulSetNameForRack(sdc.Spec.Racks[0], sdc)
		waitForStatefulSet(ctx, env, statefulSetName, scyllaDBDatacenterControllerDefaultEventuallyTimeout)
		firstMemberServiceName := naming.MemberServiceName(sdc.Spec.Racks[0], sdc, 1)
		secondMemberServiceName := naming.MemberServiceName(sdc.Spec.Racks[0], sdc, 2)
		waitForService(ctx, env, firstMemberServiceName, scyllaDBDatacenterControllerDefaultEventuallyTimeout)
		waitForService(ctx, env, secondMemberServiceName, scyllaDBDatacenterControllerDefaultEventuallyTimeout)

		g.By("Scaling the rack down to one node")
		sdc = updateRackTemplateNodes(ctx, env, sdc.Name, 1)

		g.By("Waiting for the scale down to be committed to status and the last member to be stamped")
		o.Eventually(func(eo o.Gomega, ctx context.Context) {
			decommission := getRackDecommission(ctx, eo, env, sdc.Name, sdc.Spec.Racks[0].Name)
			eo.Expect(decommission).NotTo(o.BeNil())
			eo.Expect(decommission.DesiredNodes).To(o.HaveValue(o.Equal(int32(1))))

			svc, err := env.TypedKubeClient().CoreV1().Services(env.Namespace()).Get(ctx, secondMemberServiceName, metav1.GetOptions{})
			eo.Expect(err).NotTo(o.HaveOccurred())
			eo.Expect(svc.Labels).To(o.HaveKeyWithValue(naming.DecommissionedLabel, naming.LabelValueFalse))
		}).WithContext(ctx).WithTimeout(scyllaDBDatacenterControllerDefaultEventuallyTimeout).WithPolling(100 * time.Millisecond).Should(o.Succeed())

		g.By("Scaling the rack back up to three nodes while the decommission is still in progress")
		sdc = updateRackTemplateNodes(ctx, env, sdc.Name, 3)

		g.By("Verifying the committed target holds despite the spec change")
		o.Consistently(func(co o.Gomega, ctx context.Context) {
			decommission := getRackDecommission(ctx, co, env, sdc.Name, sdc.Spec.Racks[0].Name)
			co.Expect(decommission).NotTo(o.BeNil())
			co.Expect(decommission.DesiredNodes).To(o.HaveValue(o.Equal(int32(1))))
		}).WithContext(ctx).WithTimeout(scyllaDBDatacenterControllerDefaultConsistentlyTimeout).WithPolling(100 * time.Millisecond).Should(o.Succeed())

		decommissioningServiceUIDs := map[string]types.UID{}
		for _, svcName := range []string{firstMemberServiceName, secondMemberServiceName} {
			svc, err := env.TypedKubeClient().CoreV1().Services(env.Namespace()).Get(ctx, svcName, metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			decommissioningServiceUIDs[svcName] = svc.UID
		}

		g.By("Marking the last member as decommissioned in place of the sidecar")
		markMemberServiceAsDecommissioned(ctx, env, secondMemberServiceName)

		g.By("Waiting for the next member to be stamped, continuing the committed operation past the raised node count")
		o.Eventually(func(eo o.Gomega, ctx context.Context) {
			svc, err := env.TypedKubeClient().CoreV1().Services(env.Namespace()).Get(ctx, firstMemberServiceName, metav1.GetOptions{})
			eo.Expect(err).NotTo(o.HaveOccurred())
			eo.Expect(svc.Labels).To(o.HaveKeyWithValue(naming.DecommissionedLabel, naming.LabelValueFalse))
		}).WithContext(ctx).WithTimeout(scyllaDBDatacenterControllerDefaultEventuallyTimeout).WithPolling(100 * time.Millisecond).Should(o.Succeed())

		g.By("Marking the next member as decommissioned in place of the sidecar")
		markMemberServiceAsDecommissioned(ctx, env, firstMemberServiceName)

		// The intermediate states (StatefulSet scaled to the target, services pruned) pass too quickly
		// to observe reliably against an in-process apiserver, so the operation's conclusion is asserted
		// through its end state: the record is cleared, the raised node count reconciles, and the members
		// are recreated afresh (new service UIDs, no decommission label).
		g.By("Waiting for the operation to conclude and the raised node count to reconcile with fresh members")
		o.Eventually(func(eo o.Gomega, ctx context.Context) {
			decommission := getRackDecommission(ctx, eo, env, sdc.Name, sdc.Spec.Racks[0].Name)
			eo.Expect(decommission).To(o.BeNil())

			statefulSet, err := env.TypedKubeClient().AppsV1().StatefulSets(env.Namespace()).Get(ctx, statefulSetName, metav1.GetOptions{})
			eo.Expect(err).NotTo(o.HaveOccurred())
			eo.Expect(*statefulSet.Spec.Replicas).To(o.Equal(int32(3)))

			for svcName, uid := range decommissioningServiceUIDs {
				svc, err := env.TypedKubeClient().CoreV1().Services(env.Namespace()).Get(ctx, svcName, metav1.GetOptions{})
				eo.Expect(err).NotTo(o.HaveOccurred())
				eo.Expect(svc.UID).NotTo(o.Equal(uid), "the decommissioned member's service should have been pruned and recreated for a fresh bootstrap")
				eo.Expect(svc.Labels).NotTo(o.HaveKey(naming.DecommissionedLabel))
			}
		}).WithContext(ctx).WithTimeout(scyllaDBDatacenterControllerDefaultEventuallyTimeout).WithPolling(100 * time.Millisecond).Should(o.Succeed())
	})
})

func getRackDecommission(ctx context.Context, eo o.Gomega, e *envtest.Environment, sdcName string, rackName string) *scyllav1alpha1.RackDecommissionStatus {
	sdc, err := e.ScyllaClient().ScyllaV1alpha1().ScyllaDBDatacenters(e.Namespace()).Get(ctx, sdcName, metav1.GetOptions{})
	eo.Expect(err).NotTo(o.HaveOccurred())

	rackStatus, _, ok := oslices.Find(sdc.Status.Racks, func(rackStatus scyllav1alpha1.RackStatus) bool {
		return rackStatus.Name == rackName
	})
	if !ok {
		return nil
	}
	return rackStatus.Decommission
}
